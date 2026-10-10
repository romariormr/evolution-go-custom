package autoreply

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

const (
	// Mensagem mais velha que isso é backlog da sincronização ao reconectar:
	// responder agora seria responder a conversa de ontem.
	maxMessageAge = 2 * time.Minute
	// Intervalo mínimo entre duas respostas automáticas para o mesmo contato,
	// independente da regra — trava loop entre dois bots se respondendo.
	minGapPerContact = 30 * time.Second
	rulesCacheTTL    = time.Minute
)

var ErrNotFound = errors.New("regra não encontrada")

type Sender interface {
	SendText(data *send_service.TextStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
}

// AgentChecker diz se a conversa já está com um agente humano (Chatwoot).
type AgentChecker interface {
	ConversationHasAgent(instanceId, jid string) (bool, error)
}

type cachedRules struct {
	rules    []*Rule
	loadedAt time.Time
}

type contactState struct {
	mu        sync.Mutex
	lastReply time.Time
}

type Service struct {
	db       *gorm.DB
	sender   Sender
	agents   AgentChecker
	logger   *logger_wrapper.LoggerManager
	now      func() time.Time
	cache    sync.Map // instanceId -> *cachedRules
	contacts sync.Map // instanceId|jid -> *contactState
}

func NewService(db *gorm.DB, sender Sender, agents AgentChecker, logger *logger_wrapper.LoggerManager) *Service {
	return &Service{db: db, sender: sender, agents: agents, logger: logger, now: time.Now}
}

func (s *Service) logInfo(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogInfo(format, args...)
	}
}

func (s *Service) logWarn(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogWarn(format, args...)
	}
}

// ---- CRUD ----

func (s *Service) List(instanceId string) ([]Rule, error) {
	var list []Rule
	err := s.db.Where("instance_id = ?", instanceId).Order("priority, created_at").Find(&list).Error
	return list, err
}

func (s *Service) Create(instanceId string, r *Rule) (*Rule, error) {
	if err := r.normalize(); err != nil {
		return nil, err
	}
	var n int64
	if err := s.db.Model(&Rule{}).Where("instance_id = ?", instanceId).Count(&n).Error; err != nil {
		return nil, err
	}
	if n >= maxRulesPerInst {
		return nil, errors.New("limite de regras por instância atingido")
	}
	r.Id = ""
	r.InstanceId = instanceId
	r.MatchCount = 0
	r.LastMatchAt = nil
	if err := s.db.Create(r).Error; err != nil {
		return nil, err
	}
	s.cache.Delete(instanceId)
	return r, nil
}

func (s *Service) Update(instanceId, id string, r *Rule) (*Rule, error) {
	var cur Rule
	if err := s.db.Where("instance_id = ? AND id = ?", instanceId, id).First(&cur).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := r.normalize(); err != nil {
		return nil, err
	}
	r.Id = cur.Id
	r.InstanceId = instanceId
	r.MatchCount = cur.MatchCount
	r.LastMatchAt = cur.LastMatchAt
	r.CreatedAt = cur.CreatedAt
	if err := s.db.Select("*").Omit("created_at").Save(r).Error; err != nil {
		return nil, err
	}
	s.cache.Delete(instanceId)
	return r, nil
}

func (s *Service) Delete(instanceId, id string) error {
	res := s.db.Where("instance_id = ? AND id = ?", instanceId, id).Delete(&Rule{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.db.Where("instance_id = ? AND rule_id = ?", instanceId, id).Delete(&Cooldown{})
	s.cache.Delete(instanceId)
	return nil
}

// ---- execução ----

func (s *Service) enabledRules(instanceId string) []*Rule {
	if v, ok := s.cache.Load(instanceId); ok {
		c := v.(*cachedRules)
		if s.now().Sub(c.loadedAt) < rulesCacheTTL {
			return c.rules
		}
	}
	var list []*Rule
	if err := s.db.Where("instance_id = ? AND enabled = ?", instanceId, true).Find(&list).Error; err != nil {
		s.logWarn(instanceId, "[AUTOREPLY] erro ao carregar regras: %v", err)
		return nil
	}
	sortRules(list)
	s.cache.Store(instanceId, &cachedRules{rules: list, loadedAt: s.now()})
	return list
}

func sortRules(list []*Rule) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Priority < list[j].Priority })
}

// eligible filtra o que nunca recebe resposta automática.
func eligible(info types.MessageInfo, now time.Time) bool {
	if info.IsFromMe || info.IsGroup {
		return false
	}
	if info.Chat.Server != types.DefaultUserServer && info.Chat.Server != types.HiddenUserServer {
		return false // status, canais, broadcast
	}
	if info.Type != "" && info.Type != "text" && info.Type != "media" {
		return false // reação, enquete, edição/protocolo
	}
	if !info.Timestamp.IsZero() && now.Sub(info.Timestamp) > maxMessageAge {
		return false
	}
	return true
}

// pick escolhe a primeira regra (por prioridade) que casa e está fora do cooldown.
func pick(rules []*Rule, text string, now time.Time, lastSent func(ruleId string) (time.Time, bool)) *Rule {
	for _, r := range rules {
		if !r.matches(text, now) {
			continue
		}
		if r.CooldownMinutes > 0 {
			if at, ok := lastSent(r.Id); ok && now.Sub(at) < time.Duration(r.CooldownMinutes)*time.Minute {
				continue
			}
		}
		return r
	}
	return nil
}

func (s *Service) lastSent(instanceId, jid string) func(string) (time.Time, bool) {
	return func(ruleId string) (time.Time, bool) {
		var c Cooldown
		err := s.db.Where("instance_id = ? AND jid = ? AND rule_id = ?", instanceId, jid, ruleId).First(&c).Error
		if err != nil {
			return time.Time{}, false
		}
		return c.SentAt, true
	}
}

// HandleIncoming é o hook de mensagem recebida (registrado no whatsmeow).
func (s *Service) HandleIncoming(instance *instance_model.Instance, info types.MessageInfo, text string) {
	if instance == nil {
		return
	}
	now := s.now()
	if !eligible(info, now) {
		return
	}
	rules := s.enabledRules(instance.Id)
	if len(rules) == 0 {
		return
	}
	jid := info.Chat.String()

	v, _ := s.contacts.LoadOrStore(instance.Id+"|"+jid, &contactState{})
	st := v.(*contactState)
	st.mu.Lock()
	defer st.mu.Unlock()
	if now.Sub(st.lastReply) < minGapPerContact {
		return
	}

	rule := pick(rules, text, now, s.lastSent(instance.Id, jid))
	if rule == nil {
		return
	}
	if rule.SkipIfAgent && s.agents != nil {
		has, err := s.agents.ConversationHasAgent(instance.Id, jid)
		if err != nil {
			s.logWarn(instance.Id, "[AUTOREPLY] não consegui consultar o Chatwoot (%v); respondendo assim mesmo", err)
		} else if has {
			return
		}
	}

	_, err := s.sender.SendText(&send_service.TextStruct{Number: jid, Text: render(rule.Response, info.PushName)}, instance)
	if err != nil {
		s.logWarn(instance.Id, "[AUTOREPLY] regra %q para %s falhou: %v", rule.Name, jid, err)
		return
	}
	sentAt := s.now()
	st.lastReply = sentAt
	s.logInfo(instance.Id, "[AUTOREPLY] regra %q respondeu %s", rule.Name, jid)

	if err := s.db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&Cooldown{InstanceId: instance.Id, Jid: jid, RuleId: rule.Id, SentAt: sentAt}).Error; err != nil {
		s.logWarn(instance.Id, "[AUTOREPLY] erro ao gravar cooldown: %v", err)
	}
	s.db.Model(&Rule{}).Where("id = ?", rule.Id).
		Updates(map[string]interface{}{"match_count": gorm.Expr("match_count + 1"), "last_match_at": sentAt})
}

// StartCleanup apaga cooldowns velhos (mais que o maior cooldown possível útil).
func (s *Service) StartCleanup() {
	go func() {
		for {
			cutoff := s.now().Add(-90 * 24 * time.Hour)
			if err := s.db.Where("sent_at < ?", cutoff).Delete(&Cooldown{}).Error; err != nil {
				s.logWarn("", "[AUTOREPLY] limpeza de cooldowns: %v", err)
			}
			time.Sleep(12 * time.Hour)
		}
	}()
}
