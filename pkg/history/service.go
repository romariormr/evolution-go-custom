package history

import (
	"errors"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
)

const (
	maxRawSize       = 64 * 1024
	rawRetention     = 7 * 24 * time.Hour
	cleanupInterval  = 6 * time.Hour
	defaultRetention = 90
)

var ErrNotFound = errors.New("mensagem não encontrada")

type Service struct {
	db            *gorm.DB
	logger        *logger_wrapper.LoggerManager
	retentionDays int
}

// NewService: retentionDays <= 0 usa o padrão (90 dias).
func NewService(db *gorm.DB, logger *logger_wrapper.LoggerManager, retentionDays int) *Service {
	if retentionDays <= 0 {
		retentionDays = defaultRetention
	}
	return &Service{db: db, logger: logger, retentionDays: retentionDays}
}

func (s *Service) logWarn(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogWarn(format, args...)
	}
}

// Migrate cria a tabela e o índice de busca full-text (português).
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&Entry{}); err != nil {
		return err
	}
	return db.Exec(`CREATE INDEX IF NOT EXISTS idx_message_history_fts ON message_history ` +
		`USING gin (to_tsvector('portuguese', coalesce(content, '')))`).Error
}

func skipChat(chat types.JID) bool {
	return chat.Server == types.BroadcastServer || chat.User == "status"
}

// ---- gravação ----

// entryFromEvent monta a linha de uma mensagem (nil para o que não vai pro
// histórico: reação, voto, protocolo, status).
func entryFromEvent(instanceId string, evt *events.Message, mediaURL, mediaMime string) *Entry {
	if evt == nil || evt.Message == nil || skipChat(evt.Info.Chat) {
		return nil
	}
	msgType, content, mime, fileName, quoted := describe(evt.Message)
	if msgType == "reaction" || msgType == "poll_vote" || msgType == "protocol" {
		return nil
	}
	if mediaMime != "" {
		mime = mediaMime
	}
	status := StatusReceived
	if evt.Info.IsFromMe {
		status = StatusSent
	}
	e := &Entry{
		InstanceId: instanceId,
		MessageId:  evt.Info.ID,
		ChatJid:    evt.Info.Chat.String(),
		SenderJid:  evt.Info.Sender.ToNonAD().String(),
		FromMe:     evt.Info.IsFromMe,
		IsGroup:    evt.Info.IsGroup,
		PushName:   evt.Info.PushName,
		Type:       msgType,
		Content:    clip(content),
		MediaUrl:   mediaURL,
		MediaMime:  mime,
		FileName:   fileName,
		QuotedId:   quoted,
		Status:     status,
		Source:     SourceWhatsApp,
		Timestamp:  evt.Info.Timestamp.UTC(),
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	return e
}

// OnMessage registra uma mensagem recebida (ou enviada pelo celular).
func (s *Service) OnMessage(instance *instance_model.Instance, evt *events.Message, mediaURL, mediaMime string) {
	if instance == nil || evt == nil || evt.Message == nil {
		return
	}
	if pm := evt.Message.GetProtocolMessage(); pm != nil {
		s.applyProtocol(instance.Id, pm)
		return
	}
	e := entryFromEvent(instance.Id, evt, mediaURL, mediaMime)
	if e == nil {
		return
	}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(e).Error; err != nil {
		s.logWarn(instance.Id, "[HISTORY] erro ao gravar %s: %v", evt.Info.ID, err)
	}
}

// webStatus traduz o status gravado no histórico do celular.
func webStatus(st waWeb.WebMessageInfo_Status) string {
	switch st {
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return StatusDelivered
	case waWeb.WebMessageInfo_READ:
		return StatusRead
	case waWeb.WebMessageInfo_PLAYED:
		return StatusPlayed
	}
	return StatusSent
}

// OnHistorySync importa o histórico que o celular manda (ao parear ou sob
// demanda). Mídia antiga não é baixada: fica o tipo, legenda e nome do arquivo.
func (s *Service) OnHistorySync(instance *instance_model.Instance, client *whatsmeow.Client, evt *events.HistorySync) {
	if instance == nil || client == nil || evt == nil || evt.Data == nil {
		return
	}
	var batch []*Entry
	total := 0
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(batch, 500).Error; err != nil {
			s.logWarn(instance.Id, "[HISTORY] erro ao importar histórico: %v", err)
		}
		total += len(batch)
		batch = batch[:0]
	}
	for _, conv := range evt.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil || skipChat(chat) {
			continue
		}
		for _, hm := range conv.GetMessages() {
			wm := hm.GetMessage()
			if wm == nil {
				continue
			}
			parsed, err := client.ParseWebMessage(chat, wm)
			if err != nil {
				continue
			}
			e := entryFromEvent(instance.Id, parsed, "", "")
			if e == nil {
				continue
			}
			if e.FromMe {
				e.Status = webStatus(wm.GetStatus())
			}
			batch = append(batch, e)
			if len(batch) >= 1000 {
				flush()
			}
		}
	}
	flush()
	if s.logger != nil && total > 0 {
		s.logger.GetLogger(instance.Id).LogInfo("[HISTORY] histórico importado (%s): %d mensagens", evt.Data.GetSyncType().String(), total)
	}
}

// applyProtocol reflete edição e exclusão na mensagem original.
func (s *Service) applyProtocol(instanceId string, pm *waE2E.ProtocolMessage) {
	id := pm.GetKey().GetID()
	if id == "" {
		return
	}
	q := s.db.Model(&Entry{}).Where("instance_id = ? AND message_id = ?", instanceId, id)
	switch pm.GetType() {
	case waE2E.ProtocolMessage_REVOKE:
		q.Updates(map[string]interface{}{"deleted": true})
	case waE2E.ProtocolMessage_MESSAGE_EDIT:
		_, content, _, _, _ := describe(pm.GetEditedMessage())
		if content != "" {
			q.Updates(map[string]interface{}{"content": clip(content), "edited": true})
		}
	}
}

// OnSent registra uma mensagem enviada pela API (com o proto, para retry).
func (s *Service) OnSent(instance *instance_model.Instance, info types.MessageInfo, msg *waE2E.Message) {
	if instance == nil || msg == nil || info.ID == "" || skipChat(info.Chat) {
		return
	}
	msgType, content, mime, fileName, quoted := describe(msg)
	if msgType == "reaction" || msgType == "protocol" {
		return
	}
	var raw []byte
	if b, err := proto.Marshal(msg); err == nil && len(b) <= maxRawSize {
		raw = b
	}
	ts := info.Timestamp.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	e := &Entry{
		InstanceId: instance.Id,
		MessageId:  info.ID,
		ChatJid:    info.Chat.String(),
		SenderJid:  info.Sender.ToNonAD().String(),
		FromMe:     true,
		IsGroup:    info.IsGroup,
		Type:       msgType,
		Content:    clip(content),
		MediaMime:  mime,
		FileName:   fileName,
		QuotedId:   quoted,
		Status:     StatusSent,
		Source:     SourceAPI,
		Timestamp:  ts,
		Raw:        raw,
	}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(e).Error; err != nil {
		s.logWarn(instance.Id, "[HISTORY] erro ao gravar envio %s: %v", info.ID, err)
	}
}

func receiptStatus(t types.ReceiptType) string {
	switch t {
	case types.ReceiptTypeDelivered:
		return StatusDelivered
	case types.ReceiptTypeRead:
		return StatusRead
	case types.ReceiptTypePlayed:
		return StatusPlayed
	}
	return ""
}

// OnReceipt avança o status das mensagens enviadas (nunca regride).
func (s *Service) OnReceipt(instance *instance_model.Instance, evt *events.Receipt) {
	if instance == nil || evt == nil || len(evt.MessageIDs) == 0 {
		return
	}
	status := receiptStatus(evt.Type)
	if status == "" {
		return
	}
	err := s.db.Model(&Entry{}).
		Where("instance_id = ? AND message_id IN ? AND from_me = ?", instance.Id, evt.MessageIDs, true).
		Where("(CASE status WHEN 'sent' THEN 1 WHEN 'delivered' THEN 2 WHEN 'read' THEN 3 WHEN 'played' THEN 4 ELSE 0 END) < ?", statusRank[status]).
		Updates(map[string]interface{}{"status": status}).Error
	if err != nil {
		s.logWarn(instance.Id, "[HISTORY] erro ao atualizar status: %v", err)
	}
}

// ForRetry devolve a mensagem enviada para atender um pedido de reenvio.
func (s *Service) ForRetry(instanceId string, to types.JID, id types.MessageID) *waE2E.Message {
	var e Entry
	if err := s.db.Select("raw").Where("instance_id = ? AND message_id = ? AND raw IS NOT NULL", instanceId, id).
		First(&e).Error; err != nil || len(e.Raw) == 0 {
		return nil
	}
	var m waE2E.Message
	if err := proto.Unmarshal(e.Raw, &m); err != nil {
		return nil
	}
	return &m
}

// StartCleanup apaga o que passou da retenção e o proto de retry com mais de 7 dias.
func (s *Service) StartCleanup() {
	go func() {
		for {
			now := time.Now().UTC()
			if err := s.db.Where("timestamp < ?", now.AddDate(0, 0, -s.retentionDays)).Delete(&Entry{}).Error; err != nil {
				s.logWarn("", "[HISTORY] limpeza: %v", err)
			}
			s.db.Model(&Entry{}).Where("raw IS NOT NULL AND timestamp < ?", now.Add(-rawRetention)).
				Update("raw", nil)
			time.Sleep(cleanupInterval)
		}
	}()
}

// ---- consulta ----

type Query struct {
	Chat   string
	Q      string
	Type   string
	FromMe *bool
	Before *time.Time
	After  *time.Time
	Limit  int
}

// normalizeChat aceita "5511..." ou JID completo.
func normalizeChat(chat string) []string {
	chat = strings.TrimSpace(chat)
	if chat == "" {
		return nil
	}
	if strings.Contains(chat, "@") {
		return []string{chat}
	}
	return []string{chat + "@" + types.DefaultUserServer, chat + "@" + types.GroupServer, chat + "@" + types.HiddenUserServer}
}

// Search lista mensagens da instância, mais novas primeiro (paginação por "before").
func (s *Service) Search(instanceId string, q Query) ([]Entry, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	db := s.db.Omit("raw").Where("instance_id = ?", instanceId)
	if chats := normalizeChat(q.Chat); len(chats) > 0 {
		db = db.Where("chat_jid IN ?", chats)
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		db = db.Where("(to_tsvector('portuguese', coalesce(content, '')) @@ websearch_to_tsquery('portuguese', ?) OR content ILIKE ?)",
			t, "%"+escapeLike(t)+"%")
	}
	if q.Type != "" {
		db = db.Where("type = ?", q.Type)
	}
	if q.FromMe != nil {
		db = db.Where("from_me = ?", *q.FromMe)
	}
	if q.Before != nil {
		db = db.Where("timestamp < ?", q.Before.UTC())
	}
	if q.After != nil {
		db = db.Where("timestamp > ?", q.After.UTC())
	}
	var list []Entry
	err := db.Order("timestamp DESC").Limit(q.Limit).Find(&list).Error
	return list, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Service) Get(instanceId, messageId string) (*Entry, error) {
	var e Entry
	err := s.db.Omit("raw").Where("instance_id = ? AND message_id = ?", instanceId, messageId).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &e, err
}

type ChatSummary struct {
	ChatJid     string    `json:"chatJid"`
	IsGroup     bool      `json:"isGroup"`
	PushName    string    `json:"pushName,omitempty"`
	LastType    string    `json:"lastType"`
	LastContent string    `json:"lastContent,omitempty"`
	LastFromMe  bool      `json:"lastFromMe"`
	LastAt      time.Time `json:"lastAt"`
	Messages    int64     `json:"messages"`
}

// Chats lista as conversas com a última mensagem de cada uma.
func (s *Service) Chats(instanceId string, limit int) ([]ChatSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []ChatSummary
	err := s.db.Raw(`
		SELECT l.chat_jid, l.is_group, l.push_name, l.type AS last_type, left(l.content, 200) AS last_content,
		       l.from_me AS last_from_me, l.timestamp AS last_at, c.n AS messages
		FROM (
			SELECT DISTINCT ON (chat_jid) chat_jid, is_group, push_name, type, content, from_me, timestamp
			FROM message_history WHERE instance_id = ?
			ORDER BY chat_jid, timestamp DESC
		) l
		JOIN (SELECT chat_jid, count(*) AS n FROM message_history WHERE instance_id = ? GROUP BY chat_jid) c
		  ON c.chat_jid = l.chat_jid
		ORDER BY l.timestamp DESC
		LIMIT ?`, instanceId, instanceId, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastAt.After(rows[j].LastAt) })
	return rows, nil
}
