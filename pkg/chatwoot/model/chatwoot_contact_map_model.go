package chatwoot_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChatwootContactMap guarda o vínculo entre um contato real do WhatsApp (JID)
// e o contato/conversa correspondente no Chatwoot, por instância. Uma linha
// por (instanceId, jid) — diferente do ChatwootConfig, que é uma linha só
// por instância.
type ChatwootContactMap struct {
	Id                     string `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId             string `json:"instanceId" gorm:"type:uuid;uniqueIndex:idx_chatwoot_contact_map_instance_jid;not null"`
	Jid                    string `json:"jid" gorm:"uniqueIndex:idx_chatwoot_contact_map_instance_jid;not null"`
	ChatwootContactId      string `json:"chatwootContactId"`
	ChatwootConversationId string `json:"chatwootConversationId"`

	// InboxId é a inbox em que essa conversa foi criada. Se a inbox da config
	// mudar (inbox recriada/trocada), a conversa cacheada aqui não serve mais —
	// ela vive na inbox antiga e as mensagens iriam parar num lugar que ninguém
	// olha (ou numa conversa deletada). Guardando o inbox, o cache só é reusado
	// quando bate com a inbox atual; senão contato/conversa são recriados.
	InboxId string `json:"inboxId" gorm:"default:''"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (m *ChatwootContactMap) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}
