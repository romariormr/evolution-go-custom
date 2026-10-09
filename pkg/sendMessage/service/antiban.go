package send_service

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Fila anti-ban por instância.
//
// Ideia (portada do WA-AKG, reimplementada): numa janela deslizante de
// AntiBanWindowSec segundos, se a instância já mandou AntiBanLimit mensagens, o
// próximo envio espera um atraso aleatório entre AntiBanDelayMinMs e
// AntiBanDelayMaxMs. Envios da mesma instância passam um de cada vez pela fila
// (mutex), então rajadas — campanha, n8n em loop, vários agentes — viram um fluxo
// espaçado em vez de uma saraivada, que é o padrão que o WhatsApp penaliza.
//
// Desligada por padrão: quem não liga não sente diferença de latência.

const (
	antiBanMaxDelay  = 2 * time.Minute // teto de segurança contra config absurda
	typingMsPerChar  = 40              // ~25 caracteres por segundo
	typingMaxPerSend = 4 * time.Second
	typingMinPerSend = 800 * time.Millisecond
)

type sendGate struct {
	mu   sync.Mutex
	sent []time.Time // instantes dos envios dentro da janela atual
}

var sendGates sync.Map // instanceId -> *sendGate

func gateFor(instanceId string) *sendGate {
	v, _ := sendGates.LoadOrStore(instanceId, &sendGate{})
	return v.(*sendGate)
}

// antiBanParams normaliza a config da instância (valores zerados/invertidos
// caem em defaults seguros).
func antiBanParams(inst *instance_model.Instance) (limit int, window, dMin, dMax time.Duration) {
	limit = inst.AntiBanLimit
	if limit <= 0 {
		limit = 10
	}
	window = time.Duration(inst.AntiBanWindowSec) * time.Second
	if window <= 0 {
		window = 60 * time.Second
	}
	dMin = time.Duration(inst.AntiBanDelayMinMs) * time.Millisecond
	dMax = time.Duration(inst.AntiBanDelayMaxMs) * time.Millisecond
	if dMin < 0 {
		dMin = 0
	}
	if dMax < dMin {
		dMax = dMin
	}
	if dMax > antiBanMaxDelay {
		dMax = antiBanMaxDelay
	}
	if dMin > dMax {
		dMin = dMax
	}
	return
}

// throttle segura o envio até ele poder sair, respeitando a fila anti-ban da
// instância. Deve ser chamado imediatamente antes do client.SendMessage.
// Status (status@broadcast) não passa pela fila. textLen > 0 com AntiBanTyping
// ligado simula "digitando…" proporcional ao tamanho do texto em conversa 1:1.
func (s *sendService) throttle(instance *instance_model.Instance, client *whatsmeow.Client, recipient types.JID, textLen int) {
	if instance == nil || !instance.AntiBanEnabled {
		return
	}
	if recipient.Server == types.BroadcastServer {
		return
	}

	limit, window, dMin, dMax := antiBanParams(instance)
	g := gateFor(instance.Id)

	g.mu.Lock()
	now := time.Now()
	kept := g.sent[:0]
	for _, t := range g.sent {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	g.sent = kept

	if len(g.sent) >= limit {
		wait := dMin
		if dMax > dMin {
			wait += time.Duration(rand.Int63n(int64(dMax - dMin)))
		}
		if s.loggerWrapper != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] anti-ban: %d envios nos últimos %s, aguardando %s antes de enviar para %s",
				instance.Id, len(g.sent), window, wait.Round(time.Millisecond), recipient.String())
		}
		time.Sleep(wait)
	}
	g.sent = append(g.sent, time.Now())
	g.mu.Unlock()

	// "Digitando…" fora do lock: não segura a fila dos outros envios.
	if instance.AntiBanTyping && textLen > 0 && client != nil && isPersonJID(recipient) {
		d := time.Duration(textLen*typingMsPerChar) * time.Millisecond
		if d < typingMinPerSend {
			d = typingMinPerSend
		}
		if d > typingMaxPerSend {
			d = typingMaxPerSend
		}
		_ = client.SendChatPresence(context.Background(), recipient, types.ChatPresenceComposing, types.ChatPresenceMediaText)
		time.Sleep(d)
		_ = client.SendChatPresence(context.Background(), recipient, types.ChatPresencePaused, types.ChatPresenceMediaText)
	}
}

func isPersonJID(j types.JID) bool {
	return j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer
}

// textLenOf devolve o tamanho do texto de mensagens de texto (pra simular
// digitação); 0 para os demais tipos.
func textLenOf(messageType, text string) int {
	switch messageType {
	case "ExtendedTextMessage", "Conversation":
		return len([]rune(text))
	}
	return 0
}
