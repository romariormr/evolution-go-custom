package chatwoot_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChatwootConfig guarda a configuração de integração com Chatwoot de UMA instância.
// Tabela própria (não colunas em Instance) porque cada instância pode ter conta/inbox
// diferentes no Chatwoot — ver docs/roadmap/chatwoot-spec.md, opção (A).
type ChatwootConfig struct {
	Id         string `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId string `json:"instanceId" gorm:"type:uuid;uniqueIndex;not null"`

	Enabled bool `json:"enabled" gorm:"default:false"`

	Url          string `json:"url"`
	AccountId    string `json:"accountId"`
	Token        string `json:"token"`
	SignMsg      bool   `json:"signMsg" gorm:"default:false"`
	NameInbox    string `json:"nameInbox"`
	Organization string `json:"organization"`
	Logo         string `json:"logo"`

	ConversationPending bool `json:"conversationPending" gorm:"default:false"`
	ReopenConversation  bool `json:"reopenConversation" gorm:"default:false"`

	ImportContacts          bool `json:"importContacts" gorm:"default:false"`
	ImportMessages          bool `json:"importMessages" gorm:"default:false"`
	DaysLimitImportMessages int  `json:"daysLimitImportMessages" gorm:"default:0"`

	AutoCreate bool `json:"autoCreate" gorm:"default:true"`

	// InboxId é preenchido internamente quando AutoCreate cria a inbox no Chatwoot —
	// não é informado pelo usuário.
	InboxId string `json:"inboxId" gorm:"default:''"`

	// QrConversationId é a conversa (com o contato sintético "Gerador de QR") usada
	// pra postar o QR code e os avisos de conexão direto no Chatwoot. Criada sob
	// demanda na primeira notificação, reaproveitada depois.
	QrConversationId string `json:"-" gorm:"default:''"`

	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (m *ChatwootConfig) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}
