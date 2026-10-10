package campaign

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Campanhas (disparo em massa) com acompanhamento por destinatário.
//
// Cada destinatário é uma linha em campaign_recipients; o runner pega um
// pendente por vez, envia pela fila anti-ban da instância e ainda espera um
// intervalo aleatório próprio da campanha entre um envio e outro. O estado
// fica todo no banco, então a campanha continua de onde parou depois de um
// reinício. Quem responde SAIR/PARAR entra na lista de descadastro da
// instância e é pulado nas próximas campanhas (LGPD: só dispare para quem
// consentiu receber).

const (
	StatusDraft     = "draft"
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusCanceled  = "canceled"

	RecipientPending = "pending"
	RecipientSending = "sending"
	RecipientSent    = "sent"
	RecipientFailed  = "failed"
	RecipientSkipped = "skipped"

	maxRecipients   = 10000
	defaultDelayMin = 3000
	defaultDelayMax = 8000
	maxDelayMs      = 10 * 60 * 1000
)

type Campaign struct {
	Id         string     `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId string     `json:"instanceId" gorm:"index"`
	Name       string     `json:"name"`
	Type       string     `json:"type"` // text | media
	Message    string     `json:"message" gorm:"type:text"`
	Status     string     `json:"status" gorm:"index"`
	StartAt    *time.Time `json:"startAt,omitempty"`
	DelayMinMs int        `json:"delayMinMs"`
	DelayMaxMs int        `json:"delayMaxMs"`
	Total      int        `json:"total"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	Counts     Counts     `json:"counts" gorm:"-"`
}

func (Campaign) TableName() string { return "campaigns" }

func (c *Campaign) BeforeCreate(tx *gorm.DB) error {
	if c.Id == "" {
		c.Id = uuid.New().String()
	}
	return nil
}

type Counts struct {
	Pending int `json:"pending"`
	Sending int `json:"sending"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type Recipient struct {
	Id         string     `json:"id" gorm:"type:uuid;primaryKey"`
	CampaignId string     `json:"campaignId" gorm:"index:idx_campaign_recipients_campaign_status,priority:1"`
	InstanceId string     `json:"instanceId" gorm:"index"`
	Number     string     `json:"number"`
	Name       string     `json:"name"`
	Vars       string     `json:"vars,omitempty" gorm:"type:text"`
	Status     string     `json:"status" gorm:"index:idx_campaign_recipients_campaign_status,priority:2"`
	Attempts   int        `json:"attempts"`
	MessageId  string     `json:"messageId,omitempty"`
	Error      string     `json:"error,omitempty"`
	SentAt     *time.Time `json:"sentAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func (Recipient) TableName() string { return "campaign_recipients" }

func (r *Recipient) BeforeCreate(tx *gorm.DB) error {
	if r.Id == "" {
		r.Id = uuid.New().String()
	}
	return nil
}

// OptOut: número que pediu pra não receber campanhas desta instância.
type OptOut struct {
	InstanceId string    `json:"instanceId" gorm:"primaryKey"`
	Number     string    `json:"number" gorm:"primaryKey"` // só dígitos
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (OptOut) TableName() string { return "campaign_optouts" }

// Message é o conteúdo da campanha; {{name}} e {{<var>}} são trocados por destinatário.
type Message struct {
	Text      string `json:"text,omitempty"`
	MediaUrl  string `json:"mediaUrl,omitempty"`
	MediaType string `json:"mediaType,omitempty"` // image | video | audio | document
	Caption   string `json:"caption,omitempty"`
	Filename  string `json:"filename,omitempty"`
}

type RecipientInput struct {
	Number string            `json:"number"`
	Name   string            `json:"name"`
	Vars   map[string]string `json:"vars"`
}

type CreateRequest struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	Message    Message          `json:"message"`
	Recipients []RecipientInput `json:"recipients"`
	// Numbers: alternativa simples — um número por linha (ou separados por vírgula/;).
	Numbers    string     `json:"numbers"`
	StartAt    *time.Time `json:"startAt"`
	DelayMinMs int        `json:"delayMinMs"`
	DelayMaxMs int        `json:"delayMaxMs"`
	// Start: já coloca a campanha pra rodar (senão fica em rascunho).
	Start bool `json:"start"`
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeNumber: JID fica como veio; telefone vira só dígitos.
func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "@") {
		return s
	}
	return digits(s)
}

// optOutKey: chave de descadastro (dígitos do usuário, sem servidor).
func optOutKey(number string) string {
	if i := strings.Index(number, "@"); i >= 0 {
		number = number[:i]
	}
	if i := strings.Index(number, ":"); i >= 0 {
		number = number[:i]
	}
	return digits(number)
}

func (m *Message) validate(msgType string) error {
	switch msgType {
	case "text":
		if strings.TrimSpace(m.Text) == "" {
			return errors.New("message.text é obrigatório")
		}
	case "media":
		if strings.TrimSpace(m.MediaUrl) == "" {
			return errors.New("message.mediaUrl é obrigatório")
		}
		switch m.MediaType {
		case "image", "video", "audio", "document":
		default:
			return errors.New("message.mediaType deve ser image, video, audio ou document")
		}
	default:
		return fmt.Errorf("type inválido: %q (text ou media)", msgType)
	}
	return nil
}

// build valida o pedido e devolve a campanha e os destinatários (deduplicados).
func (r *CreateRequest) build(instanceId string, now time.Time) (*Campaign, []Recipient, error) {
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	if r.Type == "" {
		r.Type = "text"
	}
	if err := r.Message.validate(r.Type); err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = "Campanha " + now.Format("02/01 15:04")
	}
	minD, maxD := r.DelayMinMs, r.DelayMaxMs
	if minD <= 0 {
		minD = defaultDelayMin
	}
	if maxD <= 0 {
		maxD = defaultDelayMax
	}
	if minD > maxDelayMs {
		minD = maxDelayMs
	}
	if maxD > maxDelayMs {
		maxD = maxDelayMs
	}
	if maxD < minD {
		maxD = minD
	}

	inputs := append([]RecipientInput(nil), r.Recipients...)
	if strings.TrimSpace(r.Numbers) != "" {
		for _, n := range strings.FieldsFunc(r.Numbers, func(c rune) bool { return c == '\n' || c == ',' || c == ';' || c == '\r' }) {
			inputs = append(inputs, RecipientInput{Number: n})
		}
	}
	seen := map[string]bool{}
	var recipients []Recipient
	for _, in := range inputs {
		num := normalizeNumber(in.Number)
		if num == "" || (!strings.Contains(num, "@") && len(num) < 8) {
			continue
		}
		if seen[num] {
			continue
		}
		seen[num] = true
		vars := ""
		if len(in.Vars) > 0 {
			b, _ := json.Marshal(in.Vars)
			vars = string(b)
		}
		recipients = append(recipients, Recipient{
			InstanceId: instanceId,
			Number:     num,
			Name:       strings.TrimSpace(in.Name),
			Vars:       vars,
			Status:     RecipientPending,
		})
	}
	if len(recipients) == 0 {
		return nil, nil, errors.New("nenhum destinatário válido")
	}
	if len(recipients) > maxRecipients {
		return nil, nil, fmt.Errorf("máximo de %d destinatários por campanha", maxRecipients)
	}

	msg, _ := json.Marshal(r.Message)
	c := &Campaign{
		InstanceId: instanceId,
		Name:       name,
		Type:       r.Type,
		Message:    string(msg),
		Status:     StatusDraft,
		DelayMinMs: minD,
		DelayMaxMs: maxD,
		Total:      len(recipients),
	}
	if r.StartAt != nil {
		at := r.StartAt.UTC()
		c.StartAt = &at
	}
	if r.Start {
		c.Status = StatusRunning
	}
	return c, recipients, nil
}

// render troca {{name}} e {{<var>}} pelos dados do destinatário.
func render(tpl string, rcp *Recipient) string {
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	out := tpl
	if rcp.Vars != "" {
		var vars map[string]string
		if json.Unmarshal([]byte(rcp.Vars), &vars) == nil {
			for k, v := range vars {
				out = strings.ReplaceAll(out, "{{"+k+"}}", v)
			}
		}
	}
	if rcp.Name == "" {
		out = strings.ReplaceAll(out, " {{name}}", "")
	}
	return strings.ReplaceAll(out, "{{name}}", rcp.Name)
}

// isOptOutText reconhece pedidos de descadastro.
func isOptOutText(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.Trim(t, ".!")
	switch t {
	case "sair", "parar", "pare", "stop", "descadastrar", "cancelar inscrição", "cancelar inscricao", "remover":
		return true
	}
	return false
}
