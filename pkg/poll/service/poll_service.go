package poll_service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	"github.com/EvolutionAPI/evolution-go/pkg/poll/model"
	"github.com/google/uuid"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// PollService define a interface para gerenciamento de votos de enquetes
type PollService interface {
	// SavePollVote salva um voto de enquete no banco de dados
	SavePollVote(ctx context.Context, vote *model.PollVote) error

	// SavePollDefinition guarda a pergunta e as opções de uma enquete no momento
	// do envio, para depois traduzir os hashes dos votos de volta para o texto.
	// Best-effort: uma falha aqui não deve impedir o envio da enquete.
	SavePollDefinition(ctx context.Context, instanceID, pollMessageID, chatJid, question string, options []string) error

	// GetPollResults retorna os resultados de uma enquete. providedOptions é
	// opcional: se o chamador informar os textos das opções, eles são usados
	// (e sobrepõem) para rotular os hashes — útil para enquetes enviadas antes
	// de existir o registro de definições.
	GetPollResults(ctx context.Context, pollMessageID, instanceID string, providedOptions []string) (*model.PollResults, error)
}

// hashOption reproduz o hash que o WhatsApp usa no voto: SHA-256 do texto da
// opção (bytes UTF-8, sem normalização). Confirmado empiricamente contra votos
// reais: sha256("Sim"), sha256("SIM") e sha256("NÃO") batem com os hashes
// armazenados — logo é case-sensitive e preserva acento/emoji.
func hashOption(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

type pollService struct {
	db            *sql.DB
	loggerWrapper *logger_wrapper.LoggerManager
}

// NewPollService cria uma nova instância do serviço de polls
func NewPollService(db *sql.DB, loggerWrapper *logger_wrapper.LoggerManager) PollService {
	service := &pollService{
		db:            db,
		loggerWrapper: loggerWrapper,
	}

	// Auto-migration: criar tabela se não existir
	if err := service.autoMigrate(); err != nil {
		loggerWrapper.GetLogger("poll-service").LogError("[POLL] Auto-migration failed: %v", err)
	}

	return service
}

// autoMigrate cria a tabela poll_votes se não existir
func (s *pollService) autoMigrate() error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS poll_votes (
			id VARCHAR(255) PRIMARY KEY,
			company_id VARCHAR(255) NOT NULL,
			instance_id VARCHAR(255) NOT NULL,
			poll_message_id VARCHAR(255) NOT NULL,
			poll_chat_jid VARCHAR(255) NOT NULL,
			vote_message_id VARCHAR(255) NOT NULL,
			voter_jid VARCHAR(255) NOT NULL,
			voter_phone VARCHAR(255),
			voter_name VARCHAR(255),
			selected_options TEXT[] NOT NULL DEFAULT '{}',
			voted_at TIMESTAMP NOT NULL DEFAULT NOW(),
			received_at TIMESTAMP NOT NULL DEFAULT NOW(),
			CONSTRAINT unique_vote_per_poll UNIQUE (poll_message_id, voter_jid)
		);
		
		CREATE INDEX IF NOT EXISTS idx_poll_votes_company ON poll_votes(company_id);
		CREATE INDEX IF NOT EXISTS idx_poll_votes_instance ON poll_votes(instance_id);
		CREATE INDEX IF NOT EXISTS idx_poll_votes_poll_message ON poll_votes(poll_message_id);
		CREATE INDEX IF NOT EXISTS idx_poll_votes_chat ON poll_votes(poll_chat_jid);
		CREATE INDEX IF NOT EXISTS idx_poll_votes_voter ON poll_votes(voter_jid);

		CREATE TABLE IF NOT EXISTS poll_options (
			poll_message_id VARCHAR(255) NOT NULL,
			instance_id VARCHAR(255) NOT NULL,
			question TEXT NOT NULL DEFAULT '',
			option_index INT NOT NULL DEFAULT 0,
			option_name TEXT NOT NULL,
			option_hash VARCHAR(64) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			CONSTRAINT unique_poll_option UNIQUE (poll_message_id, instance_id, option_hash)
		);

		CREATE INDEX IF NOT EXISTS idx_poll_options_poll ON poll_options(poll_message_id, instance_id);
	`

	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Running auto-migration...")

	_, err := s.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create poll_votes table: %w", err)
	}

	return nil
}

// SavePollVote salva um voto de enquete no banco de dados (NÃO-INVASIVO)
func (s *pollService) SavePollVote(ctx context.Context, vote *model.PollVote) error {
	// Log seguro - não expõe dados sensíveis
	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Saving vote for poll %s from %s", vote.PollMessageID, vote.VoterJid)

	// Extrair telefone do JID de forma segura
	if vote.VoterPhone == "" && strings.Contains(vote.VoterJid, "@") {
		phone := strings.Split(vote.VoterJid, "@")[0]
		vote.VoterPhone = phone
	}

	// Garantir ID único
	if vote.ID == "" {
		vote.ID = uuid.New().String()
	}

	// Query INSERT com ON CONFLICT para evitar duplicatas (SEGURO)
	query := `
		INSERT INTO poll_votes (
			id, company_id, instance_id, poll_message_id, poll_chat_jid,
			vote_message_id, voter_jid, voter_phone, voter_name,
			selected_options, voted_at, received_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12
		)
		ON CONFLICT (poll_message_id, voter_jid)
		DO UPDATE SET
			selected_options = EXCLUDED.selected_options,
			voted_at = EXCLUDED.voted_at,
			received_at = EXCLUDED.received_at
	`

	_, err := s.db.ExecContext(ctx, query,
		vote.ID,
		vote.CompanyID,
		vote.InstanceID,
		vote.PollMessageID,
		vote.PollChatJid,
		vote.VoteMessageID,
		vote.VoterJid,
		vote.VoterPhone,
		vote.VoterName,
		stringArrayToPostgresArray(vote.SelectedOptions),
		vote.VotedAt,
		vote.ReceivedAt,
	)

	if err != nil {
		s.loggerWrapper.GetLogger("poll-service").LogError("[POLL] Failed to save vote: %v", err)
		return fmt.Errorf("failed to save poll vote: %w", err)
	}

	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Vote saved successfully for poll %s", vote.PollMessageID)
	return nil
}

// SavePollDefinition guarda as opções (texto + hash) e a pergunta de uma
// enquete no envio. Best-effort e idempotente (ON CONFLICT).
func (s *pollService) SavePollDefinition(ctx context.Context, instanceID, pollMessageID, chatJid, question string, options []string) error {
	if pollMessageID == "" || len(options) == 0 {
		return nil
	}
	query := `
		INSERT INTO poll_options (poll_message_id, instance_id, question, option_index, option_name, option_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (poll_message_id, instance_id, option_hash)
		DO UPDATE SET option_name = EXCLUDED.option_name,
		              option_index = EXCLUDED.option_index,
		              question = EXCLUDED.question
	`
	for idx, name := range options {
		if _, err := s.db.ExecContext(ctx, query, pollMessageID, instanceID, question, idx, name, hashOption(name)); err != nil {
			s.loggerWrapper.GetLogger("poll-service").LogError("[POLL] Failed to save poll option: %v", err)
			return fmt.Errorf("failed to save poll option: %w", err)
		}
	}
	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Saved %d option(s) for poll %s", len(options), pollMessageID)
	return nil
}

// knownOption guarda a ordem de registro para preservar a ordem original da enquete.
type knownOption struct {
	name  string
	hash  string
	index int
}

// loadPollOptions carrega as definições guardadas de uma enquete.
func (s *pollService) loadPollOptions(ctx context.Context, pollMessageID, instanceID string) (question string, opts []knownOption) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT question, option_index, option_name, option_hash
		 FROM poll_options WHERE poll_message_id = $1 AND instance_id = $2
		 ORDER BY option_index ASC`, pollMessageID, instanceID)
	if err != nil {
		s.loggerWrapper.GetLogger("poll-service").LogWarn("[POLL] Could not load poll options: %v", err)
		return "", nil
	}
	defer rows.Close()
	for rows.Next() {
		var q, name, h string
		var idx int
		if err := rows.Scan(&q, &idx, &name, &h); err != nil {
			continue
		}
		if q != "" {
			question = q
		}
		opts = append(opts, knownOption{name: name, hash: h, index: idx})
	}
	return question, opts
}

// GetPollResults retorna os resultados agregados de uma enquete
func (s *pollService) GetPollResults(ctx context.Context, pollMessageID, instanceID string, providedOptions []string) (*model.PollResults, error) {
	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Fetching results for poll %s", pollMessageID)

	query := `
		SELECT 
			id, company_id, instance_id, poll_message_id, poll_chat_jid,
			vote_message_id, voter_jid, voter_phone, voter_name,
			selected_options, voted_at, received_at
		FROM poll_votes
		WHERE poll_message_id = $1 AND instance_id = $2
		ORDER BY voted_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, pollMessageID, instanceID)
	if err != nil {
		s.loggerWrapper.GetLogger("poll-service").LogError("[POLL] Failed to query votes: %v", err)
		return nil, fmt.Errorf("failed to query poll votes: %w", err)
	}
	defer rows.Close()

	var votes []model.PollVote
	optionCounts := make(map[string]int)

	for rows.Next() {
		var vote model.PollVote
		var selectedOptionsStr string

		err := rows.Scan(
			&vote.ID,
			&vote.CompanyID,
			&vote.InstanceID,
			&vote.PollMessageID,
			&vote.PollChatJid,
			&vote.VoteMessageID,
			&vote.VoterJid,
			&vote.VoterPhone,
			&vote.VoterName,
			&selectedOptionsStr,
			&vote.VotedAt,
			&vote.ReceivedAt,
		)
		if err != nil {
			s.loggerWrapper.GetLogger("poll-service").LogError("[POLL] Failed to scan vote: %v", err)
			continue
		}

		// Converter array do PostgreSQL para []string
		vote.SelectedOptions = postgresArrayToStringSlice(selectedOptionsStr)

		// Contar opções
		for _, option := range vote.SelectedOptions {
			optionCounts[option]++
		}

		votes = append(votes, vote)
	}

	if err = rows.Err(); err != nil {
		s.loggerWrapper.GetLogger("poll-service").LogError("[POLL] Rows iteration error: %v", err)
		return nil, fmt.Errorf("error iterating votes: %w", err)
	}

	// Rótulos: definições guardadas no envio + opções informadas no query (estas
	// sobrepõem/completam). Assim enquetes antigas (sem registro) ainda podem ser
	// rotuladas se o chamador passar as opções. Com 0 voto, ainda mostramos as
	// opções conhecidas (contagem 0).
	question, known := s.loadPollOptions(ctx, pollMessageID, instanceID)

	hashToName := make(map[string]string)
	seen := make(map[string]bool)
	order := make([]knownOption, 0, len(known)+len(providedOptions))
	addKnown := func(name, hash string, index int) {
		if hash == "" {
			return
		}
		hashToName[hash] = name
		if !seen[hash] {
			seen[hash] = true
			order = append(order, knownOption{name: name, hash: hash, index: index})
			return
		}
		for i := range order { // override do nome (provided) mantendo a posição
			if order[i].hash == hash {
				order[i].name = name
			}
		}
	}
	for _, o := range known {
		addKnown(o.name, o.hash, o.index)
	}
	for i, name := range providedOptions {
		name = strings.TrimSpace(name)
		if name != "" {
			addKnown(name, hashOption(name), 1000+i)
		}
	}

	totalVoters := len(votes)
	pct := func(c int) float64 {
		if totalVoters == 0 {
			return 0
		}
		return math.Round(float64(c)/float64(totalVoters)*1000) / 10
	}

	// Opções conhecidas (inclui as de 0 voto) + hashes votados sem rótulo.
	options := make([]model.PollOptionResult, 0, len(order)+len(optionCounts))
	for _, o := range order {
		c := optionCounts[o.hash]
		options = append(options, model.PollOptionResult{
			Name: o.name, Hash: o.hash, Count: c, Percentage: pct(c), Known: true,
		})
	}
	for hash, c := range optionCounts {
		if !seen[hash] {
			options = append(options, model.PollOptionResult{
				Name: "", Hash: hash, Count: c, Percentage: pct(c), Known: false,
			})
		}
	}
	// Ranking: mais votada primeiro; empate → conhecidas antes, depois por nome.
	sort.SliceStable(options, func(a, b int) bool {
		if options[a].Count != options[b].Count {
			return options[a].Count > options[b].Count
		}
		if options[a].Known != options[b].Known {
			return options[a].Known
		}
		return options[a].Name < options[b].Name
	})

	voters := make([]model.VoterInfo, len(votes))
	for i, vote := range votes {
		names := make([]string, len(vote.SelectedOptions))
		for j, h := range vote.SelectedOptions {
			names[j] = hashToName[h] // "" se hash desconhecido
		}
		voters[i] = model.VoterInfo{
			Jid:                 vote.VoterJid,
			Phone:               vote.VoterPhone,
			Name:                vote.VoterName,
			SelectedOptions:     vote.SelectedOptions,
			SelectedOptionNames: names,
			VotedAt:             vote.VotedAt,
		}
	}

	chatJid := ""
	if len(votes) > 0 {
		chatJid = votes[0].PollChatJid
	} else {
		votes = []model.PollVote{}
	}

	results := &model.PollResults{
		PollMessageID: pollMessageID,
		PollChatJid:   chatJid,
		Question:      question,
		TotalVotes:    totalVoters,
		TotalVoters:   totalVoters,
		Options:       options,
		Votes:         votes,
		OptionCounts:  optionCounts,
		Voters:        voters,
	}

	s.loggerWrapper.GetLogger("poll-service").LogInfo("[POLL] Poll %s: %d voter(s), %d option(s)", pollMessageID, totalVoters, len(options))
	return results, nil
}

// Helper para converter []string em formato PostgreSQL array
func stringArrayToPostgresArray(arr []string) string {
	if len(arr) == 0 {
		return "{}"
	}
	return fmt.Sprintf("{%s}", strings.Join(arr, ","))
}

// Helper para converter array PostgreSQL de volta para []string
func postgresArrayToStringSlice(s string) []string {
	// Remove { e }
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	if s == "" {
		return []string{}
	}

	return strings.Split(s, ",")
}

// BuildPollVoteFromEvent constrói um model.PollVote a partir de eventos do WhatsApp (HELPER SEGURO)
// NOTA: Espera que voteInfo já tenha passado pelo JID swap (Sender = número real)
func BuildPollVoteFromEvent(
	pollInfo *types.MessageInfo,
	voteInfo *types.MessageInfo,
	decryptedVote *waProto.PollVoteMessage,
	companyID string,
	instanceID string,
) *model.PollVote {
	// Extrair opções selecionadas (hashes SHA-256)
	selectedOptions := make([]string, len(decryptedVote.SelectedOptions))
	for i, option := range decryptedVote.SelectedOptions {
		selectedOptions[i] = fmt.Sprintf("%x", option) // Converte bytes para hex
	}

	// Extrair telefone do votante
	// NOTA: O JID swap já foi feito antes de chegar aqui!
	// Se havia LID+WhatsApp, o Sender JÁ É o número real (@s.whatsapp.net) e SenderAlt é o LID
	voterPhone := voteInfo.Sender.User

	return &model.PollVote{
		ID:              uuid.New().String(),
		CompanyID:       companyID,
		InstanceID:      instanceID,
		PollMessageID:   pollInfo.ID,
		PollChatJid:     pollInfo.Chat.String(),
		VoteMessageID:   voteInfo.ID,
		VoterJid:        voteInfo.Sender.String(),
		VoterPhone:      voterPhone,
		VoterName:       voteInfo.PushName,
		SelectedOptions: selectedOptions,
		VotedAt:         voteInfo.Timestamp,
		ReceivedAt:      time.Now(),
	}
}
