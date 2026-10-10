package webhook_producer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EvolutionAPI/evolution-go/pkg/config"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]WebhookDelivery
}

func newMem() *memStore { return &memStore{rows: map[string]WebhookDelivery{}} }

func (m *memStore) Save(d *WebhookDelivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.Id == "" {
		d.Id = time.Now().Format("150405.000000000")
	}
	m.rows[d.Id] = *d
	return nil
}
func (m *memStore) Get(instanceId, id string) (*WebhookDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.rows[id]
	if !ok || d.InstanceId != instanceId {
		return nil, errors.New("not found")
	}
	return &d, nil
}
func (m *memStore) List(instanceId, status string, limit int) ([]WebhookDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []WebhookDelivery
	for _, d := range m.rows {
		if d.InstanceId == instanceId && (status == "" || d.Status == status) {
			out = append(out, d)
		}
	}
	return out, nil
}
func (m *memStore) Cleanup(time.Time, int) (int64, error) { return 0, nil }

func testProducer(store DeliveryStore, mode string) *WebhookProducer {
	retryBackoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	return NewWebhookProducerWithDeliveries("", logger_wrapper.NewLoggerManager(&config.Config{LogDirectory: filepath.Join(os.TempDir(), "evogo-test-logs"), LogType: "console"}), store, mode)
}

func TestFailuresModeSkipsFirstTrySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	st := newMem()
	p := testProducer(st, DeliveryLogFailures)
	p.deliver(srv.URL, []byte(`{"event":"Message"}`), "inst", "message")
	if n := len(st.rows); n != 0 {
		t.Fatalf("sucesso de primeira não deveria gravar no modo failures, gravou %d", n)
	}
}

func TestFailuresModeRecordsFailedWithPayload(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	st := newMem()
	p := testProducer(st, DeliveryLogFailures)
	p.deliver(srv.URL, []byte(`{"event":"Message"}`), "inst", "message")
	list, _ := st.List("inst", DeliveryFailed, 0)
	if len(list) != 1 {
		t.Fatalf("esperava 1 entrega failed, veio %d (rows=%v)", len(list), st.rows)
	}
	d := list[0]
	if d.Attempts != 3 || d.HttpStatus != 500 || d.Payload == "" || d.Response != "boom" {
		t.Fatalf("registro incompleto: %+v", d)
	}
	if atomic.LoadInt32(&hits) != 3 {
		t.Fatalf("esperava 3 tentativas, houve %d", hits)
	}
}

func TestRetryThenSuccessEndsAsSuccessWithoutPayload(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	st := newMem()
	p := testProducer(st, DeliveryLogFailures)
	p.deliver(srv.URL, []byte(`{}`), "inst", "message")
	list, _ := st.List("inst", "", 0)
	if len(list) != 1 || list[0].Status != DeliverySuccess || list[0].Payload != "" || list[0].Attempts != 2 {
		t.Fatalf("esperava 1 registro success com 2 tentativas e sem payload: %+v", list)
	}
}

func TestResendFixesFailedDelivery(t *testing.T) {
	ok := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&ok) == 1 {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	st := newMem()
	p := testProducer(st, DeliveryLogFailures)
	p.deliver(srv.URL, []byte(`{"x":1}`), "inst", "message")
	list, _ := st.List("inst", DeliveryFailed, 0)
	if len(list) != 1 {
		t.Fatalf("precondição: 1 failed, veio %d", len(list))
	}
	atomic.StoreInt32(&ok, 1)
	d, err := p.ResendDelivery("inst", list[0].Id)
	if err != nil || d.Status != DeliverySuccess || d.Payload != "" {
		t.Fatalf("reenvio deveria dar sucesso: d=%+v err=%v", d, err)
	}
	if _, err := p.ResendDelivery("outra-instancia", list[0].Id); err == nil {
		t.Fatalf("reenvio de entrega de outra instância deveria falhar")
	}
}

func TestOffModeRecordsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	st := newMem()
	p := testProducer(st, DeliveryLogOff)
	p.deliver(srv.URL, []byte(`{}`), "inst", "message")
	if len(st.rows) != 0 {
		t.Fatalf("modo off não deveria gravar")
	}
}
