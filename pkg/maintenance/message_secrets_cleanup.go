package maintenance

import (
	"database/sql"
	"time"

	message_model "github.com/EvolutionAPI/evolution-go/pkg/message/model"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// batchSize limita quantos message_id vão por DELETE — a lib whatsmeow não
// mantém timestamp em whatsmeow_message_secrets, então a idade vem por join
// (em código, não em SQL — messages e whatsmeow_message_secrets normalmente
// vivem em bancos Postgres diferentes) com a tabela messages deste projeto.
// Sem batch, um backlog grande na primeira limpeza vira um array gigante
// numa query só.
const batchSize = 5000

// CleanupMessageSecrets apaga de whatsmeow_message_secrets (biblioteca
// whatsmeow oficial, sem mecanismo de expurgo — ver MEMORIA-PROJETO) as
// chaves de mensagens mais antigas que retentionDays, usando a tabela
// messages (deste projeto, precisa de DATABASE_SAVE_MESSAGES=true) como
// proxy de idade.
//
// ⚠️ Limitação aceita: só limpa segredos de mensagens que este projeto
// indexou em messages. Segredos de mensagens nunca indexadas (ex.: enviadas
// antes de DATABASE_SAVE_MESSAGES=true) nunca são limpos por aqui. Voto de
// poll/edição/reação chegando DEPOIS da janela de retenção pra uma mensagem
// já limpa falha silenciosamente ao descriptografar — trade-off deliberado,
// não é bug.
func CleanupMessageSecrets(db *gorm.DB, authDB *sql.DB, retentionDays int) (int64, error) {
	if retentionDays <= 0 || authDB == nil {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02 15:04:05")

	var messageIds []string
	if err := db.Model(&message_model.Message{}).Where("timestamp < ?", cutoff).Pluck("message_id", &messageIds).Error; err != nil {
		return 0, err
	}

	var totalDeleted int64
	for start := 0; start < len(messageIds); start += batchSize {
		end := start + batchSize
		if end > len(messageIds) {
			end = len(messageIds)
		}
		batch := messageIds[start:end]

		result, err := authDB.Exec(`DELETE FROM whatsmeow_message_secrets WHERE message_id = ANY($1)`, pq.Array(batch))
		if err != nil {
			return totalDeleted, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += affected
	}

	return totalDeleted, nil
}
