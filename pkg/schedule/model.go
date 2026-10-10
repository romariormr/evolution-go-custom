package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // fuso horário funciona mesmo em imagem sem tzdata

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

// Mensagens agendadas.
//
// Uma mensagem agendada guarda o mesmo corpo JSON que iria para /send/<tipo> e
// é enviada pelo worker quando chega a hora — passando pela fila anti-ban da
// instância, como qualquer envio. Pode ser única (sendAt) ou recorrente
// (cron de 5 campos, no fuso informado).

const (
	StatusScheduled = "scheduled"
	StatusSending   = "sending"
	StatusSent      = "sent"
	StatusFailed    = "failed"
	StatusPaused    = "paused"

	DefaultTimezone = "America/Sao_Paulo"

	// Envio único atrasado mais que isso (servidor parado, instância fora) não
	// sai mais: mandar "bom dia" às 15h é pior que não mandar.
	maxLateness = 6 * time.Hour
)

var SupportedTypes = []string{"text", "link", "media", "poll", "location", "contact"}

type ScheduledMessage struct {
	Id            string     `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId    string     `json:"instanceId" gorm:"index"`
	Type          string     `json:"type"`
	Number        string     `json:"number"`
	Description   string     `json:"description"`
	Payload       string     `json:"payload" gorm:"type:text"`
	SendAt        *time.Time `json:"sendAt,omitempty"`
	CronExpr      string     `json:"cronExpr,omitempty"`
	Timezone      string     `json:"timezone"`
	NextRunAt     *time.Time `json:"nextRunAt,omitempty" gorm:"index"`
	Status        string     `json:"status" gorm:"index"`
	RunCount      int        `json:"runCount"`
	LastRunAt     *time.Time `json:"lastRunAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	LastMessageId string     `json:"lastMessageId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (ScheduledMessage) TableName() string { return "scheduled_messages" }

func (m *ScheduledMessage) BeforeCreate(tx *gorm.DB) error {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return nil
}

// CreateRequest é o corpo de POST /instance/schedule/:instanceId.
type CreateRequest struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	SendAt      *time.Time      `json:"sendAt"`
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Payload     json.RawMessage `json:"payload"`
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func loadLocation(tz string) (*time.Location, error) {
	if tz == "" {
		tz = DefaultTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("fuso horário inválido: %s", tz)
	}
	return loc, nil
}

// nextRun devolve a próxima execução do cron depois de "after", no fuso tz.
func nextRun(expr, tz string, after time.Time) (time.Time, error) {
	loc, err := loadLocation(tz)
	if err != nil {
		return time.Time{}, err
	}
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron inválido: %v", err)
	}
	next := sched.Next(after.In(loc))
	if next.IsZero() {
		return time.Time{}, errors.New("cron sem próxima execução")
	}
	return next.UTC(), nil
}

func isSupported(t string) bool {
	for _, s := range SupportedTypes {
		if s == t {
			return true
		}
	}
	return false
}

// validatePayload confere se o corpo é um envio válido do tipo e devolve o número.
func validatePayload(msgType string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("payload é obrigatório")
	}
	var number string
	switch msgType {
	case "text":
		var p send_service.TextStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.Text) == "" {
			return "", errors.New("payload.text é obrigatório")
		}
		number = p.Number
	case "link":
		var p send_service.LinkStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		if p.Url == "" {
			return "", errors.New("payload.url é obrigatório")
		}
		number = p.Number
	case "media":
		var p send_service.MediaStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		if p.Url == "" || p.Type == "" {
			return "", errors.New("payload.url e payload.type são obrigatórios (envio por arquivo não é suportado no agendamento)")
		}
		number = p.Number
	case "poll":
		var p send_service.PollStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		if p.Question == "" || len(p.Options) < 2 {
			return "", errors.New("payload.question e ao menos 2 options são obrigatórios")
		}
		number = p.Number
	case "location":
		var p send_service.LocationStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		number = p.Number
	case "contact":
		var p send_service.ContactStruct
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", err
		}
		number = p.Number
	default:
		return "", fmt.Errorf("tipo não suportado: %s (use %s)", msgType, strings.Join(SupportedTypes, ", "))
	}
	if strings.TrimSpace(number) == "" {
		return "", errors.New("payload.number é obrigatório")
	}
	return number, nil
}

// build valida o pedido e monta o registro com a primeira execução calculada.
func (r *CreateRequest) build(instanceId string, now time.Time) (*ScheduledMessage, error) {
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	if !isSupported(r.Type) {
		return nil, fmt.Errorf("tipo não suportado: %q (use %s)", r.Type, strings.Join(SupportedTypes, ", "))
	}
	number, err := validatePayload(r.Type, r.Payload)
	if err != nil {
		return nil, err
	}
	tz := strings.TrimSpace(r.Timezone)
	if tz == "" {
		tz = DefaultTimezone
	}
	if _, err := loadLocation(tz); err != nil {
		return nil, err
	}

	m := &ScheduledMessage{
		InstanceId:  instanceId,
		Type:        r.Type,
		Number:      number,
		Description: strings.TrimSpace(r.Description),
		Payload:     string(r.Payload),
		Timezone:    tz,
		Status:      StatusScheduled,
	}
	expr := strings.TrimSpace(r.Cron)
	switch {
	case expr != "" && r.SendAt != nil:
		return nil, errors.New("informe sendAt (envio único) OU cron (recorrente), não os dois")
	case expr != "":
		next, err := nextRun(expr, tz, now)
		if err != nil {
			return nil, err
		}
		m.CronExpr = expr
		m.NextRunAt = &next
	case r.SendAt != nil:
		at := r.SendAt.UTC()
		if at.Before(now.Add(-time.Minute)) {
			return nil, errors.New("sendAt está no passado")
		}
		m.SendAt = &at
		m.NextRunAt = &at
	default:
		return nil, errors.New("informe sendAt (envio único) ou cron (recorrente)")
	}
	return m, nil
}
