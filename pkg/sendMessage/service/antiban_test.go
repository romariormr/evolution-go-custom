package send_service

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

func TestThrottleDisabledNoDelay(t *testing.T) {
	s := &sendService{}
	inst := &instance_model.Instance{Id: "t-disabled"}
	to := types.NewJID("5585999999999", types.DefaultUserServer)
	start := time.Now()
	for i := 0; i < 20; i++ {
		s.throttle(inst, nil, to, 0)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("fila desligada não deveria atrasar, levou %s", time.Since(start))
	}
}

func TestThrottleDelaysAfterLimit(t *testing.T) {
	s := &sendService{}
	inst := &instance_model.Instance{
		Id: "t-limit", AntiBanEnabled: true,
		AntiBanLimit: 2, AntiBanWindowSec: 60,
		AntiBanDelayMinMs: 100, AntiBanDelayMaxMs: 150,
	}
	to := types.NewJID("5585999999999", types.DefaultUserServer)

	start := time.Now()
	s.throttle(inst, nil, to, 0)
	s.throttle(inst, nil, to, 0)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("dentro do limite não deveria atrasar, levou %s", time.Since(start))
	}

	start = time.Now()
	s.throttle(inst, nil, to, 0) // 3º envio na janela -> espera 100..150ms
	el := time.Since(start)
	if el < 100*time.Millisecond || el > 400*time.Millisecond {
		t.Fatalf("3º envio deveria esperar ~100-150ms, esperou %s", el)
	}
}

func TestThrottleSkipsStatusBroadcast(t *testing.T) {
	s := &sendService{}
	inst := &instance_model.Instance{
		Id: "t-status", AntiBanEnabled: true,
		AntiBanLimit: 1, AntiBanWindowSec: 60,
		AntiBanDelayMinMs: 500, AntiBanDelayMaxMs: 500,
	}
	start := time.Now()
	for i := 0; i < 5; i++ {
		s.throttle(inst, nil, types.StatusBroadcastJID, 0)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("status não deveria passar pela fila, levou %s", time.Since(start))
	}
}

func TestAntiBanParamsDefaults(t *testing.T) {
	limit, window, dMin, dMax := antiBanParams(&instance_model.Instance{AntiBanDelayMinMs: 5000, AntiBanDelayMaxMs: 1000})
	if limit != 10 || window != 60*time.Second {
		t.Fatalf("defaults errados: limit=%d window=%s", limit, window)
	}
	if dMin > dMax {
		t.Fatalf("min > max não normalizado: %s > %s", dMin, dMax)
	}
}
