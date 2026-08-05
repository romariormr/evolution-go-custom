package message_repository

import (
	"sort"

	message_model "github.com/EvolutionAPI/evolution-go/pkg/message/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MessageRepository interface {
	InsertMessage(message message_model.Message) error
	GetMessageByID(messageID string) (*message_model.Message, error)
	DeleteAllMessages() (int64, error)
	GetLatestMessageID(source string) (string, string, error)
	GetLatestMessages(sources []string) (map[string]message_model.Message, error)
	// FindMessages lista mensagens de uma instância filtradas por remoteJid
	// (chat), paginado. Só retorna mensagens RECEBIDAS com conteúdo indexado
	// (ver whatsmeow.go, *events.Message) — mensagens enviadas via API não
	// são gravadas aqui ainda.
	FindMessages(instanceId, remoteJid string, page, limit int) ([]message_model.Message, int64, error)
}

// GetLatestMessages returns one latest row per source in a single query.
// Sources are intentionally returned with their original key so callers can
// support both canonical JIDs and legacy rows stored without the server suffix.
func (m *messageRepository) GetLatestMessages(sources []string) (map[string]message_model.Message, error) {
	result := make(map[string]message_model.Message)
	if len(sources) == 0 {
		return result, nil
	}
	unique := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source != "" {
			unique[source] = struct{}{}
		}
	}
	querySources := make([]string, 0, len(unique))
	for source := range unique {
		querySources = append(querySources, source)
	}
	rows := make([]message_model.Message, 0)
	if err := m.db.Where("source IN ?", querySources).Find(&rows).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Timestamp > rows[j].Timestamp })
	for _, row := range rows {
		if _, exists := result[row.Source]; !exists {
			result[row.Source] = row
		}
	}
	return result, nil
}

type messageRepository struct {
	db *gorm.DB
}

func (m *messageRepository) InsertMessage(message message_model.Message) error {
	return m.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"timestamp", "status", "source"}),
	}).Create(&message).Error
}

func (m *messageRepository) FindMessages(instanceId, remoteJid string, page, limit int) ([]message_model.Message, int64, error) {
	query := m.db.Model(&message_model.Message{}).Where("instance_id = ?", instanceId)
	if remoteJid != "" {
		query = query.Where("source = ?", remoteJid)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var messages []message_model.Message
	err := query.Order("timestamp DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, 0, err
	}

	return messages, total, nil
}

func (m *messageRepository) GetMessageByID(messageID string) (*message_model.Message, error) {
	var message message_model.Message
	err := m.db.Where("message_id = ?", messageID).First(&message).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &message, nil
}

func (m *messageRepository) DeleteAllMessages() (int64, error) {
	result := m.db.Exec("DELETE FROM messages")
	return result.RowsAffected, result.Error
}

func (m *messageRepository) GetLatestMessageID(source string) (string, string, error) {
	var message message_model.Message
	err := m.db.Where("source = ?", source).Order("timestamp DESC").First(&message).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", "", nil
		}
		return "", "", err
	}

	return message.MessageID, message.Timestamp, nil
}

func NewMessageRepository(db *gorm.DB) MessageRepository {
	return &messageRepository{db: db}
}
