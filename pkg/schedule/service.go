package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

const (
	pollInterval       = 15 * time.Second
	claimBatch         = 50
	maxParallelSends   = 4
	maxActivePerInst   = 500
	stuckSendingCutoff = 10 * time.Minute
)

var (
	ErrNotFound     = errors.New("agendamento não encontrado")
	ErrLimitReached = fmt.Errorf("limite de %d agendamentos ativos por instância atingido", maxActivePerInst)
)

// Sender é o subconjunto do SendService usado pelo agendador.
type Sender interface {
	SendText(data *send_service.TextStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendLink(data *send_service.LinkStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendMediaUrl(data *send_service.MediaStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendPoll(data *send_service.PollStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendLocation(data *send_service.LocationStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendContact(data *send_service.ContactStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
}

type InstanceGetter interface {
	GetInstanceByID(instanceId string) (*instance_model.Instance, error)
}

type Service struct {
	db        *gorm.DB
	sender    Sender
	instances InstanceGetter
	logger    *logger_wrapper.LoggerManager
	now       func() time.Time
	startOnce sync.Once
}

func NewService(db *gorm.DB, sender Sender, instances InstanceGetter, logger *logger_wrapper.LoggerManager) *Service {
	return &Service{db: db, sender: sender, instances: instances, logger: logger, now: time.Now}
}

func (s *Service) logInfo(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogInfo(format, args...)
	}
}

func (s *Service) logError(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogError(format, args...)
	}
}

// ---- CRUD ----

func (s *Service) Create(instanceId string, req *CreateRequest) (*ScheduledMessage, error) {
	m, err := req.build(instanceId, s.now())
	if err != nil {
		return nil, err
	}
	var active int64
	if err := s.db.Model(&ScheduledMessage{}).
		Where("instance_id = ? AND status IN ?", instanceId, []string{StatusScheduled, StatusPaused}).
		Count(&active).Error; err != nil {
		return nil, err
	}
	if active >= maxActivePerInst {
		return nil, ErrLimitReached
	}
	if err := s.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) List(instanceId, status string, limit int) ([]ScheduledMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := s.db.Where("instance_id = ?", instanceId)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var list []ScheduledMessage
	err := q.Order("CASE WHEN next_run_at IS NULL THEN 1 ELSE 0 END, next_run_at, updated_at DESC").
		Limit(limit).Find(&list).Error
	return list, err
}

func (s *Service) Get(instanceId, id string) (*ScheduledMessage, error) {
	var m ScheduledMessage
	err := s.db.Where("instance_id = ? AND id = ?", instanceId, id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &m, err
}

// Delete remove o agendamento (não dá pra apagar um que está saindo agora).
func (s *Service) Delete(instanceId, id string) error {
	res := s.db.Where("instance_id = ? AND id = ? AND status <> ?", instanceId, id, StatusSending).
		Delete(&ScheduledMessage{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Pause(instanceId, id string) (*ScheduledMessage, error) {
	res := s.db.Model(&ScheduledMessage{}).
		Where("instance_id = ? AND id = ? AND status = ?", instanceId, id, StatusScheduled).
		Update("status", StatusPaused)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("só dá pra pausar um agendamento ativo")
	}
	return s.Get(instanceId, id)
}

// Resume reativa um agendamento pausado. Recorrente: recalcula a próxima
// execução a partir de agora. Único: só volta se o horário ainda não passou.
func (s *Service) Resume(instanceId, id string) (*ScheduledMessage, error) {
	m, err := s.Get(instanceId, id)
	if err != nil {
		return nil, err
	}
	if m.Status != StatusPaused {
		return nil, errors.New("só dá pra retomar um agendamento pausado")
	}
	now := s.now()
	var next time.Time
	if m.CronExpr != "" {
		if next, err = nextRun(m.CronExpr, m.Timezone, now); err != nil {
			return nil, err
		}
	} else {
		if m.SendAt == nil || m.SendAt.Before(now) {
			return nil, errors.New("o horário deste envio único já passou; crie um novo agendamento")
		}
		next = *m.SendAt
	}
	if err := s.db.Model(&ScheduledMessage{}).Where("id = ? AND status = ?", id, StatusPaused).
		Updates(map[string]interface{}{"status": StatusScheduled, "next_run_at": next}).Error; err != nil {
		return nil, err
	}
	return s.Get(instanceId, id)
}

// ---- worker ----

// Start sobe o worker que dispara os agendamentos vencidos.
func (s *Service) Start() {
	s.startOnce.Do(func() {
		s.recoverStuck()
		go func() {
			ticker := time.NewTicker(pollInterval)
			defer ticker.Stop()
			for {
				s.RunDue()
				<-ticker.C
			}
		}()
	})
}

// recoverStuck: envio único que ficou "sending" (servidor caiu no meio) vira
// falha — não reenvia sozinho pra não correr o risco de mandar em dobro.
func (s *Service) recoverStuck() {
	err := s.db.Model(&ScheduledMessage{}).
		Where("status = ? AND updated_at < ?", StatusSending, s.now().Add(-stuckSendingCutoff)).
		Updates(map[string]interface{}{"status": StatusFailed, "last_error": "interrompido: o servidor reiniciou durante o envio"}).Error
	if err != nil {
		s.logError("", "[SCHEDULE] recover: %v", err)
	}
}

// claimDue pega os vencidos com FOR UPDATE SKIP LOCKED (seguro com mais de uma
// réplica) e já avança o estado de cada um antes de enviar: único vira
// "sending"; recorrente ganha a próxima execução (atrasos não acumulam: depois
// de um tempo parado sai uma vez só).
func (s *Service) claimDue(now time.Time) ([]ScheduledMessage, error) {
	var due []ScheduledMessage
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []ScheduledMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_run_at <= ?", StatusScheduled, now).
			Order("next_run_at").Limit(claimBatch).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			updates, send := advance(r, now)
			if err := tx.Model(&ScheduledMessage{}).Where("id = ?", r.Id).Updates(updates).Error; err != nil {
				return err
			}
			if send {
				due = append(due, r)
			}
		}
		return nil
	})
	return due, err
}

// advance decide o novo estado de um agendamento vencido e se ele deve sair agora.
func advance(r ScheduledMessage, now time.Time) (map[string]interface{}, bool) {
	if r.CronExpr != "" {
		next, err := nextRun(r.CronExpr, r.Timezone, now)
		if err != nil {
			return map[string]interface{}{"status": StatusFailed, "last_error": err.Error(), "next_run_at": nil}, false
		}
		return map[string]interface{}{"next_run_at": next}, true
	}
	if r.NextRunAt != nil && now.Sub(*r.NextRunAt) > maxLateness {
		return map[string]interface{}{
			"status":      StatusFailed,
			"next_run_at": nil,
			"last_error":  fmt.Sprintf("não enviado: atrasou mais de %s (servidor/instância indisponível no horário)", maxLateness),
		}, false
	}
	return map[string]interface{}{"status": StatusSending, "next_run_at": nil}, true
}

// RunDue dispara tudo que venceu. Exportado pra testes/diagnóstico.
func (s *Service) RunDue() {
	due, err := s.claimDue(s.now().UTC())
	if err != nil {
		s.logError("", "[SCHEDULE] erro ao buscar agendamentos: %v", err)
		return
	}
	if len(due) == 0 {
		return
	}
	sem := make(chan struct{}, maxParallelSends)
	var wg sync.WaitGroup
	for _, m := range due {
		m := m
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.execute(m)
		}()
	}
	wg.Wait()
}

func (s *Service) execute(m ScheduledMessage) {
	msgId, err := s.dispatch(m)
	now := s.now().UTC()
	updates := map[string]interface{}{
		"last_run_at":     now,
		"run_count":       gorm.Expr("run_count + 1"),
		"last_error":      "",
		"last_message_id": msgId,
	}
	if err != nil {
		updates["last_error"] = err.Error()
		s.logError(m.InstanceId, "[SCHEDULE] %s (%s para %s) falhou: %v", m.Id, m.Type, m.Number, err)
	} else {
		s.logInfo(m.InstanceId, "[SCHEDULE] %s (%s para %s) enviado: %s", m.Id, m.Type, m.Number, msgId)
	}
	q := s.db.Model(&ScheduledMessage{}).Where("id = ?", m.Id)
	if m.CronExpr == "" {
		if err != nil {
			updates["status"] = StatusFailed
		} else {
			updates["status"] = StatusSent
		}
		q = q.Where("status = ?", StatusSending)
	}
	if dbErr := q.Updates(updates).Error; dbErr != nil {
		s.logError(m.InstanceId, "[SCHEDULE] erro ao gravar resultado de %s: %v", m.Id, dbErr)
	}
}

func decode[T any](raw string) (*T, error) {
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("payload inválido: %v", err)
	}
	return &v, nil
}

// dispatch envia pelo SendService (que passa pela fila anti-ban da instância).
func (s *Service) dispatch(m ScheduledMessage) (string, error) {
	inst, err := s.instances.GetInstanceByID(m.InstanceId)
	if err != nil {
		return "", fmt.Errorf("instância não encontrada: %v", err)
	}
	var res *send_service.MessageSendStruct
	switch m.Type {
	case "text":
		p, e := decode[send_service.TextStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendText(p, inst)
	case "link":
		p, e := decode[send_service.LinkStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendLink(p, inst)
	case "media":
		p, e := decode[send_service.MediaStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendMediaUrl(p, inst)
	case "poll":
		p, e := decode[send_service.PollStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendPoll(p, inst)
	case "location":
		p, e := decode[send_service.LocationStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendLocation(p, inst)
	case "contact":
		p, e := decode[send_service.ContactStruct](m.Payload)
		if e != nil {
			return "", e
		}
		res, err = s.sender.SendContact(p, inst)
	default:
		return "", fmt.Errorf("tipo não suportado: %s", m.Type)
	}
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Info.ID, nil
}
