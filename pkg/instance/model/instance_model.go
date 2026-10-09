package instance_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Instance struct {
	Id               string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name             string    `json:"name"`
	Token            string    `json:"token" gorm:"unique"`
	Webhook          string    `json:"webhook"`
	Webhooks         []string  `json:"webhooks" gorm:"type:text;serializer:json"`
	RabbitmqEnable   string    `json:"rabbitmqEnable"`
	WebSocketEnable  string    `json:"websocketEnable"`
	NatsEnable       string    `json:"natsEnable"`
	Jid              string    `json:"jid" gorm:"column:jid"`
	Qrcode           string    `json:"qrcode" gorm:"type:text"`
	Connected        bool      `json:"connected"`
	Expiration       int64     `json:"expiration"`
	DisconnectReason string    `json:"disconnect_reason"`
	Events           string    `json:"events"`
	OsName           string    `json:"os_name"`
	Proxy            string    `json:"proxy"`
	ClientName       string    `json:"client_name"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`

	// Advanced Settings
	AlwaysOnline  bool   `json:"alwaysOnline" gorm:"default:false"`
	RejectCall    bool   `json:"rejectCall" gorm:"default:false"`
	MsgRejectCall string `json:"msgRejectCall" gorm:"default:''"`
	ReadMessages  bool   `json:"readMessages" gorm:"default:false"`
	IgnoreGroups  bool   `json:"ignoreGroups" gorm:"default:false"`
	IgnoreStatus  bool   `json:"ignoreStatus" gorm:"default:false"`

	// Anti-ban: fila de envio por instância. Desligada por padrão (não muda a
	// latência de quem não usa). Ligada, se já saíram AntiBanLimit mensagens nos
	// últimos AntiBanWindowSec segundos, o próximo envio espera um atraso aleatório
	// entre AntiBanDelayMinMs e AntiBanDelayMaxMs. AntiBanTyping simula "digitando…"
	// antes de mensagens de texto pra contato 1:1.
	AntiBanEnabled    bool `json:"antiBanEnabled" gorm:"default:false"`
	AntiBanLimit      int  `json:"antiBanLimit" gorm:"default:10"`
	AntiBanWindowSec  int  `json:"antiBanWindowSec" gorm:"default:60"`
	AntiBanDelayMinMs int  `json:"antiBanDelayMinMs" gorm:"default:2000"`
	AntiBanDelayMaxMs int  `json:"antiBanDelayMaxMs" gorm:"default:6000"`
	AntiBanTyping     bool `json:"antiBanTyping" gorm:"default:false"`
}

// AdvancedSettings representa as configurações avançadas de uma instância
type AdvancedSettings struct {
	AlwaysOnline  bool   `json:"alwaysOnline"`
	RejectCall    bool   `json:"rejectCall"`
	MsgRejectCall string `json:"msgRejectCall"`
	ReadMessages  bool   `json:"readMessages"`
	IgnoreGroups  bool   `json:"ignoreGroups"`
	IgnoreStatus  bool   `json:"ignoreStatus"`

	AntiBanEnabled    bool `json:"antiBanEnabled"`
	AntiBanLimit      int  `json:"antiBanLimit"`
	AntiBanWindowSec  int  `json:"antiBanWindowSec"`
	AntiBanDelayMinMs int  `json:"antiBanDelayMinMs"`
	AntiBanDelayMaxMs int  `json:"antiBanDelayMaxMs"`
	AntiBanTyping     bool `json:"antiBanTyping"`
}

func (m *Instance) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}
