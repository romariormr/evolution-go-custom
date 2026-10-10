package webhook_producer

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Log de entregas de webhook.
//
// Cada tentativa de entregar um evento num webhook pode ser registrada aqui, com
// código HTTP, erro, trecho da resposta e latência — pra investigar por que um
// n8n/Chatwoot/integração "não recebeu" um evento, e reenviar o que falhou.
//
// Modo (env WEBHOOK_DELIVERY_LOG):
//   - "failures" (padrão): só grava quando a entrega falha (com o payload, pra
//     poder reenviar). Uma entrega que falha e depois dá certo no retry fica
//     registrada como sucesso.
//   - "all": grava toda entrega (payload só fica guardado enquanto não deu certo).
//   - "off": não grava nada.

const (
	DeliveryLogFailures = "failures"
	DeliveryLogAll      = "all"
	DeliveryLogOff      = "off"

	DeliveryPending = "pending"
	DeliverySuccess = "success"
	DeliveryFailed  = "failed"

	maxStoredResponse = 1024
)

type WebhookDelivery struct {
	Id         string    `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId string    `json:"instanceId" gorm:"index:idx_webhook_deliveries_instance_created,priority:1"`
	Url        string    `json:"url"`
	Event      string    `json:"event"`
	Status     string    `json:"status" gorm:"index"`
	Attempts   int       `json:"attempts"`
	HttpStatus int       `json:"httpStatus"`
	LastError  string    `json:"lastError"`
	Response   string    `json:"response"`
	LatencyMs  int64     `json:"latencyMs"`
	Payload    string    `json:"payload,omitempty" gorm:"type:text"`
	CreatedAt  time.Time `json:"createdAt" gorm:"index:idx_webhook_deliveries_instance_created,priority:2"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

func (d *WebhookDelivery) BeforeCreate(tx *gorm.DB) error {
	if d.Id == "" {
		d.Id = uuid.New().String()
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DeliveryStore persiste o log de entregas. Implementação GORM abaixo; nil
// desliga o log sem quebrar o envio.
type DeliveryStore interface {
	Save(d *WebhookDelivery) error
	Get(instanceId, id string) (*WebhookDelivery, error)
	List(instanceId, status string, limit int) ([]WebhookDelivery, error)
	Cleanup(olderThan time.Time, keepPerInstance int) (int64, error)
}

type gormDeliveryStore struct{ db *gorm.DB }

func NewGormDeliveryStore(db *gorm.DB) DeliveryStore {
	if db == nil {
		return nil
	}
	return &gormDeliveryStore{db: db}
}

func (s *gormDeliveryStore) Save(d *WebhookDelivery) error {
	return s.db.Save(d).Error
}

func (s *gormDeliveryStore) Get(instanceId, id string) (*WebhookDelivery, error) {
	var d WebhookDelivery
	err := s.db.Where("instance_id = ? AND id = ?", instanceId, id).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *gormDeliveryStore) List(instanceId, status string, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := s.db.Where("instance_id = ?", instanceId)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var out []WebhookDelivery
	// Payload fica de fora da listagem (pode ser grande); vem no reenvio/detalhe.
	err := q.Omit("payload").Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Cleanup apaga entregas mais velhas que olderThan e, por instância, as que
// passarem de keepPerInstance (mantém as mais recentes).
func (s *gormDeliveryStore) Cleanup(olderThan time.Time, keepPerInstance int) (int64, error) {
	res := s.db.Where("created_at < ?", olderThan).Delete(&WebhookDelivery{})
	if res.Error != nil {
		return 0, res.Error
	}
	total := res.RowsAffected
	if keepPerInstance > 0 {
		res = s.db.Exec(`DELETE FROM webhook_deliveries WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY instance_id ORDER BY created_at DESC) AS rn
				FROM webhook_deliveries
			) ranked WHERE rn > ?
		)`, keepPerInstance)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
	}
	return total, nil
}
