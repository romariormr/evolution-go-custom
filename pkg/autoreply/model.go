package autoreply

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Respostas automáticas por instância.
//
// Gatilhos:
//   - keyword: a mensagem casa com o padrão (contains/equals/startsWith/regex RE2).
//   - away:    qualquer mensagem recebida FORA do horário de atendimento.
//   - welcome: qualquer mensagem de um contato que não recebeu esta resposta
//     dentro do cooldown (ex.: 1440 min = uma saudação por dia por contato).
//
// Uma mensagem dispara no máximo uma regra (a de menor prioridade que casar e
// estiver fora do cooldown para aquele contato). Grupos, status, canais e
// mensagens antigas (sincronização ao reconectar) são ignorados.

const (
	TriggerKeyword = "keyword"
	TriggerAway    = "away"
	TriggerWelcome = "welcome"

	MatchContains   = "contains"
	MatchEquals     = "equals"
	MatchStartsWith = "startsWith"
	MatchRegex      = "regex"

	DefaultTimezone = "America/Sao_Paulo"
	maxResponseLen  = 4096
	maxRulesPerInst = 100
)

type Rule struct {
	Id              string     `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId      string     `json:"instanceId" gorm:"index"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	Trigger         string     `json:"trigger"`
	MatchType       string     `json:"matchType"`
	Pattern         string     `json:"pattern"`
	Response        string     `json:"response" gorm:"type:text"`
	Priority        int        `json:"priority"`
	CooldownMinutes int        `json:"cooldownMinutes"`
	SkipIfAgent     bool       `json:"skipIfAgent"`
	Timezone        string     `json:"timezone"`
	Days            string     `json:"days"`      // dias de atendimento, 0=domingo..6=sábado, ex. "1,2,3,4,5"
	StartTime       string     `json:"startTime"` // "08:00"
	EndTime         string     `json:"endTime"`   // "18:00"
	MatchCount      int        `json:"matchCount"`
	LastMatchAt     *time.Time `json:"lastMatchAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`

	re *regexp.Regexp `gorm:"-"`
}

func (Rule) TableName() string { return "auto_reply_rules" }

func (r *Rule) BeforeCreate(tx *gorm.DB) error {
	if r.Id == "" {
		r.Id = uuid.New().String()
	}
	return nil
}

// Cooldown registra a última resposta de uma regra para um contato.
type Cooldown struct {
	InstanceId string    `gorm:"primaryKey"`
	Jid        string    `gorm:"primaryKey"`
	RuleId     string    `gorm:"primaryKey"`
	SentAt     time.Time `gorm:"index"`
}

func (Cooldown) TableName() string { return "auto_reply_cooldowns" }

// normalize valida a regra e preenche os padrões.
func (r *Rule) normalize() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Trigger = strings.TrimSpace(r.Trigger)
	r.Response = strings.TrimSpace(r.Response)
	if r.Response == "" {
		return errors.New("response é obrigatório")
	}
	if len(r.Response) > maxResponseLen {
		return fmt.Errorf("response passa de %d caracteres", maxResponseLen)
	}
	if r.Timezone == "" {
		r.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return fmt.Errorf("fuso horário inválido: %s", r.Timezone)
	}
	if r.CooldownMinutes < 0 {
		r.CooldownMinutes = 0
	}

	switch r.Trigger {
	case TriggerKeyword:
		if r.MatchType == "" {
			r.MatchType = MatchContains
		}
		if strings.TrimSpace(r.Pattern) == "" {
			return errors.New("pattern é obrigatório para trigger keyword")
		}
		switch r.MatchType {
		case MatchContains, MatchEquals, MatchStartsWith:
		case MatchRegex:
			if _, err := regexp.Compile(r.Pattern); err != nil {
				return fmt.Errorf("regex inválida: %v", err)
			}
		default:
			return fmt.Errorf("matchType inválido: %s (contains, equals, startsWith, regex)", r.MatchType)
		}
		if r.Priority == 0 {
			r.Priority = 10
		}
	case TriggerAway:
		if r.Days == "" {
			r.Days = "1,2,3,4,5"
		}
		if r.StartTime == "" {
			r.StartTime = "08:00"
		}
		if r.EndTime == "" {
			r.EndTime = "18:00"
		}
		if _, err := parseDays(r.Days); err != nil {
			return err
		}
		if _, err := parseClock(r.StartTime); err != nil {
			return err
		}
		if _, err := parseClock(r.EndTime); err != nil {
			return err
		}
		if r.Priority == 0 {
			r.Priority = 20
		}
		if r.CooldownMinutes == 0 {
			r.CooldownMinutes = 240
		}
	case TriggerWelcome:
		if r.Priority == 0 {
			r.Priority = 30
		}
		if r.CooldownMinutes == 0 {
			r.CooldownMinutes = 1440
		}
	default:
		return fmt.Errorf("trigger inválido: %q (keyword, away, welcome)", r.Trigger)
	}
	return nil
}

func parseDays(s string) (map[time.Weekday]bool, error) {
	out := map[time.Weekday]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 6 {
			return nil, fmt.Errorf("days inválido: %q (use 0=domingo..6=sábado, separados por vírgula)", s)
		}
		out[time.Weekday(n)] = true
	}
	return out, nil
}

// parseClock devolve minutos desde a meia-noite de "HH:MM".
func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("horário inválido: %q (use HH:MM)", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// withinBusinessHours diz se "now" está dentro do horário de atendimento da regra.
// Suporta virada de dia (ex.: 22:00 às 06:00).
func (r *Rule) withinBusinessHours(now time.Time) bool {
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	days, _ := parseDays(r.Days)
	start, _ := parseClock(r.StartTime)
	end, _ := parseClock(r.EndTime)
	mins := local.Hour()*60 + local.Minute()
	if start <= end {
		return days[local.Weekday()] && mins >= start && mins < end
	}
	// janela atravessa a meia-noite: depois do início conta pro dia atual,
	// antes do fim conta pro dia anterior
	if mins >= start {
		return days[local.Weekday()]
	}
	if mins < end {
		return days[(local.Weekday()+6)%7]
	}
	return false
}

// matches diz se a regra dispara para o texto recebido em "now".
func (r *Rule) matches(text string, now time.Time) bool {
	switch r.Trigger {
	case TriggerKeyword:
		if text == "" {
			return false
		}
		t := strings.ToLower(strings.TrimSpace(text))
		p := strings.ToLower(strings.TrimSpace(r.Pattern))
		switch r.MatchType {
		case MatchEquals:
			return t == p
		case MatchStartsWith:
			return strings.HasPrefix(t, p)
		case MatchRegex:
			if r.re == nil {
				re, err := regexp.Compile(r.Pattern)
				if err != nil {
					return false
				}
				r.re = re
			}
			return r.re.MatchString(text)
		default:
			return strings.Contains(t, p)
		}
	case TriggerAway:
		return !r.withinBusinessHours(now)
	case TriggerWelcome:
		return true
	}
	return false
}

// render troca {{name}} pelo nome do contato (sem nome, some junto com o espaço antes).
func render(response, pushName string) string {
	name := strings.TrimSpace(pushName)
	if name == "" {
		response = strings.ReplaceAll(response, " {{name}}", "")
	}
	return strings.ReplaceAll(response, "{{name}}", name)
}
