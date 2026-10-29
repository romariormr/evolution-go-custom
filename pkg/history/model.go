package history

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"gorm.io/gorm"
)

// Histórico de mensagens.
//
// Uma linha por mensagem (recebida, enviada pela API ou enviada pelo celular),
// por instância, com tipo, texto/legenda, URL da mídia no MinIO (quando o
// download de mídia está ligado), citação e status de entrega atualizado
// pelos recibos (enviada -> entregue -> lida -> ouvida). Edições e mensagens
// apagadas são refletidas na linha original.
//
// Busca por texto usa o full-text do próprio Postgres (to_tsvector
// 'portuguese' + índice GIN) — não precisa de extensão (pgvector/pg_trgm).
// Mídia não fica no banco: só a URL do objeto no MinIO.

const (
	StatusReceived  = "received"
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusRead      = "read"
	StatusPlayed    = "played"

	SourceWhatsApp = "whatsapp" // recebida, ou enviada pelo celular/WhatsApp Web
	SourceAPI      = "api"      // enviada por este Evolution GO

	maxContentLen = 16 * 1024
)

type Entry struct {
	Id         string    `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId string    `json:"instanceId" gorm:"uniqueIndex:idx_message_history_inst_msg,priority:1;index:idx_message_history_chat,priority:1"`
	MessageId  string    `json:"messageId" gorm:"uniqueIndex:idx_message_history_inst_msg,priority:2"`
	ChatJid    string    `json:"chatJid" gorm:"index:idx_message_history_chat,priority:2"`
	SenderJid  string    `json:"senderJid,omitempty"`
	FromMe     bool      `json:"fromMe"`
	IsGroup    bool      `json:"isGroup"`
	PushName   string    `json:"pushName,omitempty"`
	Type       string    `json:"type"`
	Content    string    `json:"content,omitempty" gorm:"type:text"`
	MediaUrl   string    `json:"mediaUrl,omitempty" gorm:"type:text"`
	MediaMime  string    `json:"mediaMime,omitempty"`
	FileName   string    `json:"fileName,omitempty"`
	QuotedId   string    `json:"quotedId,omitempty"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	Edited     bool      `json:"edited"`
	Deleted    bool      `json:"deleted"`
	Timestamp  time.Time `json:"timestamp" gorm:"index:idx_message_history_chat,priority:3"`
	// Raw: proto da mensagem enviada pela API, guardado por poucos dias para
	// atender pedidos de reenvio (GetMessageForRetry). Nunca sai na API.
	Raw       []byte    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Entry) TableName() string { return "message_history" }

func (e *Entry) BeforeCreate(tx *gorm.DB) error {
	if e.Id == "" {
		e.Id = uuid.New().String()
	}
	return nil
}

// statusRank: status só avança (um "entregue" atrasado não desfaz um "lida").
var statusRank = map[string]int{StatusSent: 1, StatusDelivered: 2, StatusRead: 3, StatusPlayed: 4}

// describe extrai tipo, texto, mime, nome de arquivo e mensagem citada.
func describe(m *waE2E.Message) (msgType, content, mime, fileName, quoted string) {
	if m == nil {
		return "unknown", "", "", "", ""
	}
	if inner := m.GetDocumentWithCaptionMessage().GetMessage(); inner != nil {
		m = inner
	}
	if inner := m.GetEphemeralMessage().GetMessage(); inner != nil {
		m = inner
	}
	if inner := m.GetViewOnceMessage().GetMessage(); inner != nil {
		m = inner
	}
	if inner := m.GetViewOnceMessageV2().GetMessage(); inner != nil {
		m = inner
	}
	switch {
	case m.Conversation != nil:
		return "text", m.GetConversation(), "", "", ""
	case m.ExtendedTextMessage != nil:
		x := m.GetExtendedTextMessage()
		return "text", x.GetText(), "", "", x.GetContextInfo().GetStanzaID()
	case m.ImageMessage != nil:
		x := m.GetImageMessage()
		return "image", x.GetCaption(), x.GetMimetype(), "", x.GetContextInfo().GetStanzaID()
	case m.VideoMessage != nil:
		x := m.GetVideoMessage()
		return "video", x.GetCaption(), x.GetMimetype(), "", x.GetContextInfo().GetStanzaID()
	case m.PtvMessage != nil:
		x := m.GetPtvMessage()
		return "video", x.GetCaption(), x.GetMimetype(), "", x.GetContextInfo().GetStanzaID()
	case m.AudioMessage != nil:
		x := m.GetAudioMessage()
		return "audio", "", x.GetMimetype(), "", x.GetContextInfo().GetStanzaID()
	case m.DocumentMessage != nil:
		x := m.GetDocumentMessage()
		return "document", x.GetCaption(), x.GetMimetype(), x.GetFileName(), x.GetContextInfo().GetStanzaID()
	case m.StickerMessage != nil:
		x := m.GetStickerMessage()
		return "sticker", "", x.GetMimetype(), "", x.GetContextInfo().GetStanzaID()
	case m.LocationMessage != nil:
		x := m.GetLocationMessage()
		return "location", strings.TrimSpace(x.GetName() + " " + x.GetAddress()), "", "", ""
	case m.LiveLocationMessage != nil:
		return "location", m.GetLiveLocationMessage().GetCaption(), "", "", ""
	case m.ContactMessage != nil:
		return "contact", m.GetContactMessage().GetDisplayName(), "", "", ""
	case m.ContactsArrayMessage != nil:
		var names []string
		for _, c := range m.GetContactsArrayMessage().GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		return "contact", strings.Join(names, ", "), "", "", ""
	case m.PollCreationMessage != nil:
		return "poll", m.GetPollCreationMessage().GetName(), "", "", ""
	case m.PollCreationMessageV2 != nil:
		return "poll", m.GetPollCreationMessageV2().GetName(), "", "", ""
	case m.PollCreationMessageV3 != nil:
		return "poll", m.GetPollCreationMessageV3().GetName(), "", "", ""
	case m.ButtonsResponseMessage != nil:
		return "buttons_response", m.GetButtonsResponseMessage().GetSelectedDisplayText(), "", "", m.GetButtonsResponseMessage().GetContextInfo().GetStanzaID()
	case m.ListResponseMessage != nil:
		return "list_response", m.GetListResponseMessage().GetTitle(), "", "", m.GetListResponseMessage().GetContextInfo().GetStanzaID()
	case m.TemplateButtonReplyMessage != nil:
		return "buttons_response", m.GetTemplateButtonReplyMessage().GetSelectedDisplayText(), "", "", ""
	case m.InteractiveResponseMessage != nil:
		return "interactive_response", m.GetInteractiveResponseMessage().GetBody().GetText(), "", "", ""
	case m.ButtonsMessage != nil:
		return "buttons", m.GetButtonsMessage().GetContentText(), "", "", ""
	case m.ListMessage != nil:
		return "list", m.GetListMessage().GetDescription(), "", "", ""
	case m.InteractiveMessage != nil:
		return "interactive", m.GetInteractiveMessage().GetBody().GetText(), "", "", ""
	case m.ReactionMessage != nil:
		return "reaction", m.GetReactionMessage().GetText(), "", "", m.GetReactionMessage().GetKey().GetID()
	case m.ProtocolMessage != nil:
		return "protocol", "", "", "", ""
	case m.PollUpdateMessage != nil:
		return "poll_vote", "", "", "", ""
	case m.AlbumMessage != nil:
		return "album", "", "", "", ""
	}
	return "other", "", "", "", ""
}

func clip(s string) string {
	if len(s) <= maxContentLen {
		return s
	}
	return s[:maxContentLen]
}
