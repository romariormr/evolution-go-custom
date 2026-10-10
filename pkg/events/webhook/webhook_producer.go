package webhook_producer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	producer_interfaces "github.com/EvolutionAPI/evolution-go/pkg/events/interfaces"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
)

// Espera entre tentativas (crescente). Total ~3 min antes de desistir — cobre
// reinício rápido do destino (n8n, Chatwoot) sem martelar um serviço fora do ar.
var retryBackoff = []time.Duration{5 * time.Second, 15 * time.Second, 45 * time.Second, 2 * time.Minute}

// Timeout por requisição. Sem isso um destino que aceita a conexão e não
// responde prendia a goroutine pra sempre.
const requestTimeout = 15 * time.Second

// WebhookProducer entrega eventos nos webhooks e (opcionalmente) registra o
// resultado de cada entrega em DeliveryStore.
type WebhookProducer struct {
	url           string
	loggerWrapper *logger_wrapper.LoggerManager
	httpClient    *http.Client
	store         DeliveryStore
	logMode       string
}

func NewWebhookProducer(
	url string,
	loggerWrapper *logger_wrapper.LoggerManager,
) producer_interfaces.Producer {
	return NewWebhookProducerWithDeliveries(url, loggerWrapper, nil, DeliveryLogOff)
}

// NewWebhookProducerWithDeliveries cria o produtor com log de entregas. store nil
// ou logMode "off" desligam o log. Inicia a limpeza periódica do log.
func NewWebhookProducerWithDeliveries(
	url string,
	loggerWrapper *logger_wrapper.LoggerManager,
	store DeliveryStore,
	logMode string,
) *WebhookProducer {
	switch logMode {
	case DeliveryLogAll, DeliveryLogOff:
	default:
		logMode = DeliveryLogFailures
	}
	if store == nil {
		logMode = DeliveryLogOff
	}
	p := &WebhookProducer{
		url:           url,
		loggerWrapper: loggerWrapper,
		httpClient:    &http.Client{Timeout: requestTimeout},
		store:         store,
		logMode:       logMode,
	}
	return p
}

// StartCleanup apaga periodicamente entregas antigas (retentionDays) e mantém no
// máximo keepPerInstance por instância.
func (p *WebhookProducer) StartCleanup(retentionDays, keepPerInstance int) {
	if p.store == nil {
		return
	}
	if retentionDays <= 0 {
		retentionDays = 7
	}
	run := func() {
		n, err := p.store.Cleanup(time.Now().AddDate(0, 0, -retentionDays), keepPerInstance)
		if err != nil {
			fmt.Printf("[WEBHOOK] limpeza do log de entregas falhou: %v\n", err)
			return
		}
		if n > 0 {
			fmt.Printf("[WEBHOOK] limpeza do log de entregas: %d removidas\n", n)
		}
	}
	go func() {
		run()
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for range t.C {
			run()
		}
	}()
}

func (p *WebhookProducer) Produce(
	queueName string,
	payload []byte,
	webhookUrl string,
	userID string,
) error {
	splitQueue := strings.Split(queueName, ".")

	if len(splitQueue) < 2 {
		return nil
	}
	event := splitQueue[len(splitQueue)-1]

	if p.url != "" {
		go p.deliver(p.url, payload, userID, event)
	}
	if webhookUrl != "" {
		go p.deliver(webhookUrl, payload, userID, event)
	}

	return nil
}

// deliver tenta entregar com retry e registra o resultado conforme logMode.
func (p *WebhookProducer) deliver(url string, body []byte, userID, event string) {
	var rec *WebhookDelivery
	record := func(attempt int, res attemptResult, status string) {
		if p.logMode == DeliveryLogOff {
			return
		}
		if rec == nil {
			if p.logMode == DeliveryLogFailures && status == DeliverySuccess {
				return // sucesso de primeira não interessa no modo "failures"
			}
			rec = &WebhookDelivery{InstanceId: userID, Url: url, Event: event, Payload: string(body)}
		}
		rec.Attempts = attempt
		rec.Status = status
		rec.HttpStatus = res.status
		rec.LatencyMs = res.latency.Milliseconds()
		rec.Response = truncate(string(res.body), maxStoredResponse)
		rec.LastError = ""
		if res.err != nil {
			rec.LastError = truncate(res.err.Error(), maxStoredResponse)
		}
		if status == DeliverySuccess {
			rec.Payload = "" // entregue: não precisa guardar pra reenvio
		}
		if err := p.store.Save(rec); err != nil {
			p.loggerWrapper.GetLogger(userID).LogWarn("[%s] webhook: falha ao gravar log de entrega: %v", userID, err)
		}
	}

	maxAttempts := len(retryBackoff) + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res := p.send(url, body)
		if res.err == nil {
			p.loggerWrapper.GetLogger(userID).LogInfo("[%s] webhook sent successfully - url: %s, status: %d, response: %s", userID, url, res.status, truncate(string(res.body), 200))
			record(attempt, res, DeliverySuccess)
			return
		}
		p.loggerWrapper.GetLogger(userID).LogWarn("[%s] webhook failed - url: %s, attempt: %d/%d, error: %v", userID, url, attempt, maxAttempts, res.err)

		if attempt == maxAttempts {
			record(attempt, res, DeliveryFailed)
			break
		}
		record(attempt, res, DeliveryPending)
		time.Sleep(retryBackoff[attempt-1])
	}
	p.loggerWrapper.GetLogger(userID).LogError("[%s] webhook failed after maximum retries - url: %s", userID, url)
}

type attemptResult struct {
	err     error
	status  int
	body    []byte
	latency time.Duration
}

func (p *WebhookProducer) send(url string, body []byte) attemptResult {
	start := time.Now()
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return attemptResult{err: err, latency: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return attemptResult{err: err, latency: time.Since(start)}
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	lat := time.Since(start)
	if err != nil {
		return attemptResult{err: fmt.Errorf("erro ao ler resposta: %v", err), status: resp.StatusCode, latency: lat}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return attemptResult{err: errors.New("received non-2xx response: " + resp.Status), status: resp.StatusCode, body: responseBody, latency: lat}
	}
	return attemptResult{status: resp.StatusCode, body: responseBody, latency: lat}
}

// ── API do log de entregas (usada pelos handlers da instância) ─────────────

// ErrDeliveryLogDisabled indica que o log de entregas está desligado.
var ErrDeliveryLogDisabled = errors.New("log de entregas de webhook desligado (WEBHOOK_DELIVERY_LOG=off)")

func (p *WebhookProducer) ListDeliveries(instanceId, status string, limit int) ([]WebhookDelivery, error) {
	if p.store == nil {
		return nil, ErrDeliveryLogDisabled
	}
	return p.store.List(instanceId, status, limit)
}

// ResendDelivery reenvia (uma tentativa, síncrona) o payload de uma entrega
// registrada e atualiza o registro com o resultado.
func (p *WebhookProducer) ResendDelivery(instanceId, deliveryId string) (*WebhookDelivery, error) {
	if p.store == nil {
		return nil, ErrDeliveryLogDisabled
	}
	d, err := p.store.Get(instanceId, deliveryId)
	if err != nil {
		return nil, err
	}
	if d.Payload == "" {
		return d, errors.New("essa entrega não tem payload guardado (já foi entregue com sucesso)")
	}
	res := p.send(d.Url, []byte(d.Payload))
	d.Attempts++
	d.HttpStatus = res.status
	d.LatencyMs = res.latency.Milliseconds()
	d.Response = truncate(string(res.body), maxStoredResponse)
	if res.err != nil {
		d.Status = DeliveryFailed
		d.LastError = truncate(res.err.Error(), maxStoredResponse)
	} else {
		d.Status = DeliverySuccess
		d.LastError = ""
		d.Payload = ""
	}
	if err := p.store.Save(d); err != nil {
		return d, err
	}
	return d, nil
}

// TestResult é o resultado de um disparo de teste para uma URL.
type TestResult struct {
	Url        string `json:"url"`
	Ok         bool   `json:"ok"`
	HttpStatus int    `json:"httpStatus"`
	LatencyMs  int64  `json:"latencyMs"`
	Error      string `json:"error,omitempty"`
	Response   string `json:"response,omitempty"`
}

// TestWebhooks manda um evento "WebhookTest" (uma tentativa, síncrona) para
// cada URL e devolve o resultado. Sempre registra no log (se ligado).
func (p *WebhookProducer) TestWebhooks(instanceId string, urls []string) []TestResult {
	payload, _ := json.Marshal(map[string]any{
		"event":      "WebhookTest",
		"instanceId": instanceId,
		"data": map[string]any{
			"message":   "Teste de webhook do Evolution GO",
			"timestamp": time.Now().Unix(),
		},
	})
	out := make([]TestResult, 0, len(urls))
	for _, u := range urls {
		if strings.TrimSpace(u) == "" {
			continue
		}
		res := p.send(u, payload)
		tr := TestResult{Url: u, Ok: res.err == nil, HttpStatus: res.status, LatencyMs: res.latency.Milliseconds(), Response: truncate(string(res.body), maxStoredResponse)}
		if res.err != nil {
			tr.Error = res.err.Error()
		}
		out = append(out, tr)
		if p.store != nil && p.logMode != DeliveryLogOff {
			d := &WebhookDelivery{InstanceId: instanceId, Url: u, Event: "webhooktest", Attempts: 1,
				HttpStatus: res.status, LatencyMs: tr.LatencyMs, Response: tr.Response}
			if tr.Ok {
				d.Status = DeliverySuccess
			} else {
				d.Status, d.LastError, d.Payload = DeliveryFailed, tr.Error, string(payload)
			}
			_ = p.store.Save(d)
		}
	}
	return out
}

// CreateGlobalQueues não faz nada para webhook producer
func (p *WebhookProducer) CreateGlobalQueues() error {
	return nil
}
