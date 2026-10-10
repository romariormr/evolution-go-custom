package campaign

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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
	tickInterval        = 5 * time.Second
	sleepStep           = 2 * time.Second
	stuckSendingCutoff  = 10 * time.Minute
	maxConsecutiveFails = 5
)

var ErrNotFound = errors.New("campanha não encontrada")

type Sender interface {
	SendText(data *send_service.TextStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
	SendMediaUrl(data *send_service.MediaStruct, instance *instance_model.Instance) (*send_service.MessageSendStruct, error)
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
	active    sync.Map // campaignId -> struct{}
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

func (s *Service) logWarn(instanceId, format string, args ...interface{}) {
	if s.logger != nil {
		s.logger.GetLogger(instanceId).LogWarn(format, args...)
	}
}

// ---- CRUD ----

func (s *Service) optOutSet(instanceId string) (map[string]bool, error) {
	var list []OptOut
	if err := s.db.Where("instance_id = ?", instanceId).Find(&list).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(list))
	for _, o := range list {
		set[o.Number] = true
	}
	return set, nil
}

func (s *Service) Create(instanceId string, req *CreateRequest) (*Campaign, error) {
	c, recipients, err := req.build(instanceId, s.now())
	if err != nil {
		return nil, err
	}
	optOuts, err := s.optOutSet(instanceId)
	if err != nil {
		return nil, err
	}
	for i := range recipients {
		if optOuts[optOutKey(recipients[i].Number)] {
			recipients[i].Status = RecipientSkipped
			recipients[i].Error = "descadastrado (pediu para não receber)"
		}
	}
	if c.Status == StatusRunning {
		now := s.now().UTC()
		c.StartedAt = &now
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		for i := range recipients {
			recipients[i].CampaignId = c.Id
		}
		return tx.CreateInBatches(recipients, 500).Error
	})
	if err != nil {
		return nil, err
	}
	s.fillCounts([]*Campaign{c})
	return c, nil
}

func (s *Service) fillCounts(list []*Campaign) {
	if len(list) == 0 {
		return
	}
	ids := make([]string, len(list))
	byId := map[string]*Campaign{}
	for i, c := range list {
		ids[i] = c.Id
		byId[c.Id] = c
	}
	var rows []struct {
		CampaignId string
		Status     string
		N          int
	}
	s.db.Model(&Recipient{}).Select("campaign_id, status, count(*) as n").
		Where("campaign_id IN ?", ids).Group("campaign_id, status").Scan(&rows)
	for _, r := range rows {
		c := byId[r.CampaignId]
		switch r.Status {
		case RecipientPending:
			c.Counts.Pending = r.N
		case RecipientSending:
			c.Counts.Sending = r.N
		case RecipientSent:
			c.Counts.Sent = r.N
		case RecipientFailed:
			c.Counts.Failed = r.N
		case RecipientSkipped:
			c.Counts.Skipped = r.N
		}
	}
}

func (s *Service) List(instanceId string) ([]*Campaign, error) {
	var list []*Campaign
	if err := s.db.Where("instance_id = ?", instanceId).Order("created_at DESC").Limit(200).Find(&list).Error; err != nil {
		return nil, err
	}
	s.fillCounts(list)
	return list, nil
}

func (s *Service) Get(instanceId, id string) (*Campaign, error) {
	var c Campaign
	err := s.db.Where("instance_id = ? AND id = ?", instanceId, id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.fillCounts([]*Campaign{&c})
	return &c, nil
}

func (s *Service) Recipients(instanceId, id, status string, limit, offset int) ([]Recipient, error) {
	if _, err := s.Get(instanceId, id); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := s.db.Where("campaign_id = ?", id)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var list []Recipient
	err := q.Order("created_at, id").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

// transition muda o status se o atual estiver em "from".
func (s *Service) transition(instanceId, id string, from []string, updates map[string]interface{}) (*Campaign, error) {
	res := s.db.Model(&Campaign{}).Where("instance_id = ? AND id = ? AND status IN ?", instanceId, id, from).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		if _, err := s.Get(instanceId, id); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("operação não permitida no status atual")
	}
	return s.Get(instanceId, id)
}

func (s *Service) Start(instanceId, id string) (*Campaign, error) {
	c, err := s.transition(instanceId, id, []string{StatusDraft, StatusPaused},
		map[string]interface{}{"status": StatusRunning, "last_error": ""})
	if err == nil && c.StartedAt == nil {
		now := s.now().UTC()
		s.db.Model(&Campaign{}).Where("id = ?", id).Update("started_at", now)
		c.StartedAt = &now
	}
	return c, err
}

func (s *Service) Pause(instanceId, id string) (*Campaign, error) {
	return s.transition(instanceId, id, []string{StatusRunning}, map[string]interface{}{"status": StatusPaused})
}

func (s *Service) Cancel(instanceId, id string) (*Campaign, error) {
	now := s.now().UTC()
	c, err := s.transition(instanceId, id, []string{StatusDraft, StatusRunning, StatusPaused},
		map[string]interface{}{"status": StatusCanceled, "finished_at": now})
	if err != nil {
		return nil, err
	}
	s.db.Model(&Recipient{}).Where("campaign_id = ? AND status = ?", id, RecipientPending).
		Updates(map[string]interface{}{"status": RecipientSkipped, "error": "campanha cancelada"})
	s.fillCounts([]*Campaign{c})
	return c, nil
}

// Retry devolve os que falharam para a fila e volta a campanha a rodar.
func (s *Service) Retry(instanceId, id string) (*Campaign, error) {
	c, err := s.Get(instanceId, id)
	if err != nil {
		return nil, err
	}
	if c.Status == StatusCanceled {
		return nil, errors.New("campanha cancelada")
	}
	res := s.db.Model(&Recipient{}).Where("campaign_id = ? AND status = ?", id, RecipientFailed).
		Updates(map[string]interface{}{"status": RecipientPending, "error": ""})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("nenhum destinatário com falha")
	}
	s.db.Model(&Campaign{}).Where("id = ?", id).
		Updates(map[string]interface{}{"status": StatusRunning, "finished_at": nil, "last_error": ""})
	return s.Get(instanceId, id)
}

func (s *Service) Delete(instanceId, id string) error {
	c, err := s.Get(instanceId, id)
	if err != nil {
		return err
	}
	if c.Status == StatusRunning {
		return errors.New("pause ou cancele a campanha antes de apagar")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("campaign_id = ?", id).Delete(&Recipient{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Campaign{}).Error
	})
}

// ---- descadastro ----

func (s *Service) ListOptOuts(instanceId string) ([]OptOut, error) {
	var list []OptOut
	err := s.db.Where("instance_id = ?", instanceId).Order("created_at DESC").Limit(5000).Find(&list).Error
	return list, err
}

func (s *Service) AddOptOut(instanceId, number, reason string) error {
	key := optOutKey(number)
	if key == "" {
		return errors.New("número inválido")
	}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&OptOut{InstanceId: instanceId, Number: key, Reason: reason}).Error; err != nil {
		return err
	}
	// tira da fila das campanhas em andamento desta instância
	var pending []Recipient
	s.db.Where("instance_id = ? AND status = ?", instanceId, RecipientPending).
		Select("id", "number").Find(&pending)
	var ids []string
	for _, r := range pending {
		if optOutKey(r.Number) == key {
			ids = append(ids, r.Id)
		}
	}
	if len(ids) > 0 {
		s.db.Model(&Recipient{}).Where("id IN ?", ids).
			Updates(map[string]interface{}{"status": RecipientSkipped, "error": "descadastrado (pediu para não receber)"})
	}
	return nil
}

func (s *Service) RemoveOptOut(instanceId, number string) error {
	return s.db.Where("instance_id = ? AND number = ?", instanceId, optOutKey(number)).Delete(&OptOut{}).Error
}

// HandleIncoming registra descadastro quando o contato responde SAIR/PARAR.
func (s *Service) HandleIncoming(instance *instance_model.Instance, info types.MessageInfo, text string) {
	if instance == nil || info.IsFromMe || info.IsGroup || !isOptOutText(text) {
		return
	}
	for _, j := range []types.JID{info.Chat, info.Sender, info.SenderAlt} {
		if j.User == "" || (j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer) {
			continue
		}
		if err := s.AddOptOut(instance.Id, j.User, "respondeu: "+text); err != nil {
			s.logWarn(instance.Id, "[CAMPAIGN] erro ao registrar descadastro de %s: %v", j.User, err)
		}
	}
	s.logInfo(instance.Id, "[CAMPAIGN] %s pediu descadastro", info.Chat.String())
}

// ---- runner ----

// StartWorker sobe o loop que executa as campanhas em andamento.
func (s *Service) StartWorker() {
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(tickInterval)
			defer ticker.Stop()
			for {
				s.tick()
				<-ticker.C
			}
		}()
	})
}

func (s *Service) tick() {
	now := s.now().UTC()
	// envio interrompido por reinício vira falha (pode ser reenviado com "retry")
	s.db.Model(&Recipient{}).Where("status = ? AND updated_at < ?", RecipientSending, now.Add(-stuckSendingCutoff)).
		Updates(map[string]interface{}{"status": RecipientFailed, "error": "interrompido: o servidor reiniciou durante o envio"})

	var running []Campaign
	if err := s.db.Where("status = ? AND (start_at IS NULL OR start_at <= ?)", StatusRunning, now).
		Find(&running).Error; err != nil {
		s.logWarn("", "[CAMPAIGN] erro ao buscar campanhas: %v", err)
		return
	}
	for _, c := range running {
		if _, loaded := s.active.LoadOrStore(c.Id, struct{}{}); loaded {
			continue
		}
		go func(c Campaign) {
			defer s.active.Delete(c.Id)
			s.run(c)
		}(c)
	}
}

func (s *Service) status(id string) string {
	var c Campaign
	if err := s.db.Select("status").Where("id = ?", id).First(&c).Error; err != nil {
		return ""
	}
	return c.Status
}

// claimNext pega o próximo pendente (SKIP LOCKED: seguro com mais de uma réplica).
func (s *Service) claimNext(campaignId string) (*Recipient, error) {
	var r Recipient
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("campaign_id = ? AND status = ?", campaignId, RecipientPending).
			Order("created_at, id").Limit(1).Find(&r).Error; err != nil {
			return err
		}
		if r.Id == "" {
			return nil
		}
		return tx.Model(&Recipient{}).Where("id = ?", r.Id).
			Updates(map[string]interface{}{"status": RecipientSending, "attempts": gorm.Expr("attempts + 1")}).Error
	})
	if err != nil || r.Id == "" {
		return nil, err
	}
	return &r, nil
}

// wait dorme o intervalo da campanha em passos curtos, saindo cedo se ela parar.
func (s *Service) wait(c *Campaign) bool {
	d := time.Duration(c.DelayMinMs) * time.Millisecond
	if c.DelayMaxMs > c.DelayMinMs {
		d += time.Duration(rand.Intn(c.DelayMaxMs-c.DelayMinMs+1)) * time.Millisecond
	}
	for d > 0 {
		step := sleepStep
		if d < step {
			step = d
		}
		time.Sleep(step)
		d -= step
		if s.status(c.Id) != StatusRunning {
			return false
		}
	}
	return true
}

func (s *Service) run(c Campaign) {
	var msg Message
	if err := json.Unmarshal([]byte(c.Message), &msg); err != nil {
		s.db.Model(&Campaign{}).Where("id = ?", c.Id).
			Updates(map[string]interface{}{"status": StatusPaused, "last_error": "mensagem inválida: " + err.Error()})
		return
	}
	s.logInfo(c.InstanceId, "[CAMPAIGN] %q (%s) em execução", c.Name, c.Id)
	fails := 0
	for {
		if s.status(c.Id) != StatusRunning {
			return
		}
		r, err := s.claimNext(c.Id)
		if err != nil {
			s.logWarn(c.InstanceId, "[CAMPAIGN] erro ao pegar destinatário: %v", err)
			return
		}
		if r == nil {
			var sending int64
			s.db.Model(&Recipient{}).Where("campaign_id = ? AND status = ?", c.Id, RecipientSending).Count(&sending)
			if sending == 0 {
				now := s.now().UTC()
				s.db.Model(&Campaign{}).Where("id = ? AND status = ?", c.Id, StatusRunning).
					Updates(map[string]interface{}{"status": StatusCompleted, "finished_at": now})
				s.logInfo(c.InstanceId, "[CAMPAIGN] %q concluída", c.Name)
			}
			return
		}

		var optOut int64
		s.db.Model(&OptOut{}).Where("instance_id = ? AND number = ?", c.InstanceId, optOutKey(r.Number)).Count(&optOut)
		if optOut > 0 {
			s.db.Model(&Recipient{}).Where("id = ?", r.Id).
				Updates(map[string]interface{}{"status": RecipientSkipped, "error": "descadastrado (pediu para não receber)"})
			continue
		}

		msgId, sendErr := s.send(&c, &msg, r)
		now := s.now().UTC()
		if sendErr != nil {
			fails++
			s.db.Model(&Recipient{}).Where("id = ?", r.Id).
				Updates(map[string]interface{}{"status": RecipientFailed, "error": sendErr.Error()})
			s.logWarn(c.InstanceId, "[CAMPAIGN] %q -> %s falhou: %v", c.Name, r.Number, sendErr)
			if fails >= maxConsecutiveFails {
				s.db.Model(&Campaign{}).Where("id = ? AND status = ?", c.Id, StatusRunning).
					Updates(map[string]interface{}{
						"status":     StatusPaused,
						"last_error": fmt.Sprintf("pausada após %d falhas seguidas (último erro: %v)", fails, sendErr),
					})
				return
			}
		} else {
			fails = 0
			s.db.Model(&Recipient{}).Where("id = ?", r.Id).
				Updates(map[string]interface{}{"status": RecipientSent, "message_id": msgId, "sent_at": now, "error": ""})
		}
		if !s.wait(&c) {
			return
		}
	}
}

func (s *Service) send(c *Campaign, msg *Message, r *Recipient) (string, error) {
	inst, err := s.instances.GetInstanceByID(c.InstanceId)
	if err != nil {
		return "", fmt.Errorf("instância não encontrada: %v", err)
	}
	var res *send_service.MessageSendStruct
	switch c.Type {
	case "media":
		res, err = s.sender.SendMediaUrl(&send_service.MediaStruct{
			Number:   r.Number,
			Url:      msg.MediaUrl,
			Type:     msg.MediaType,
			Caption:  render(msg.Caption, r),
			Filename: msg.Filename,
		}, inst)
	default:
		res, err = s.sender.SendText(&send_service.TextStruct{Number: r.Number, Text: render(msg.Text, r)}, inst)
	}
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Info.ID, nil
}
