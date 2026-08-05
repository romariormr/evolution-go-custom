package message_model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Message struct {
	Id        string `json:"id" gorm:"type:uuid;primaryKey"`
	MessageID string `json:"message_id" gorm:"unique"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	// Campos abaixo só são preenchidos no recebimento (*events.Message em
	// whatsmeow.go) — os inserts de recibo (Receipt: Delivered/Read) não os
	// tocam, então nunca sobrescrevem com vazio (ver DoUpdates em
	// message_repository.go, que só atualiza timestamp/status/source).
	InstanceId string `json:"instanceId"`
	FromMe     bool   `json:"fromMe"`
	IsGroup    bool   `json:"isGroup"`
	PushName   string `json:"pushName"`
	Content    string `json:"content" gorm:"type:text"`
}

func (m *Message) BeforeCreate(tx *gorm.DB) (err error) {
	m.Id = uuid.New().String()
	return
}
