package access_service

import (
	"strings"
	"sync"
	"time"
)

// Proteção contra força bruta no login: depois de loginMaxFailures erros
// para o mesmo usuário vindo do mesmo IP dentro de loginWindow, bloqueia por
// loginLockFor. Um limite maior (loginMaxFailuresUser) vale para o usuário
// vindo de qualquer IP. Em memória: reiniciar o serviço zera os contadores.

const (
	loginMaxFailures     = 5
	loginMaxFailuresUser = 20
	loginWindow          = 15 * time.Minute
	loginLockFor         = 15 * time.Minute
	challengeTTL         = 5 * time.Minute
)

type failEntry struct {
	count       int
	first       time.Time
	lockedUntil time.Time
}

type loginGuard struct {
	mu    sync.Mutex
	fails map[string]*failEntry
}

func newLoginGuard() *loginGuard { return &loginGuard{fails: map[string]*failEntry{}} }

func guardKeys(identifier, ip string) (string, string) {
	id := strings.ToLower(strings.TrimSpace(identifier))
	return "u:" + id + "|" + ip, "u:" + id
}

// locked devolve até quando está bloqueado (zero = liberado).
func (g *loginGuard) locked(identifier, ip string, now time.Time) time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	var until time.Time
	k1, k2 := guardKeys(identifier, ip)
	for _, k := range []string{k1, k2} {
		if e := g.fails[k]; e != nil && now.Before(e.lockedUntil) && e.lockedUntil.After(until) {
			until = e.lockedUntil
		}
	}
	return until
}

func (g *loginGuard) fail(identifier, ip string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k1, k2 := guardKeys(identifier, ip)
	for k, max := range map[string]int{k1: loginMaxFailures, k2: loginMaxFailuresUser} {
		e := g.fails[k]
		if e == nil || now.Sub(e.first) > loginWindow {
			e = &failEntry{first: now}
			g.fails[k] = e
		}
		e.count++
		if e.count >= max {
			e.lockedUntil = now.Add(loginLockFor)
			e.count = 0
			e.first = now
		}
	}
	// limpeza simples para o mapa não crescer sem limite
	if len(g.fails) > 10000 {
		for k, e := range g.fails {
			if now.Sub(e.first) > loginWindow && now.After(e.lockedUntil) {
				delete(g.fails, k)
			}
		}
	}
}

func (g *loginGuard) success(identifier, ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k1, _ := guardKeys(identifier, ip)
	delete(g.fails, k1)
}

// Desafio de 2FA: depois de senha certa, o navegador recebe um código
// temporário e precisa voltar com o código do app autenticador.
type challenge struct {
	userId     string
	identifier string
	expires    time.Time
}

type challengeStore struct {
	mu sync.Mutex
	m  map[string]challenge
}

func newChallengeStore() *challengeStore { return &challengeStore{m: map[string]challenge{}} }

func (c *challengeStore) put(userId, identifier string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	tok := randomToken(24)
	c.m[tok] = challenge{userId: userId, identifier: identifier, expires: now.Add(challengeTTL)}
	return tok
}

func (c *challengeStore) take(tok string) (challenge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[tok]
	if !ok || time.Now().After(v.expires) {
		delete(c.m, tok)
		return challenge{}, false
	}
	return v, true
}

func (c *challengeStore) drop(tok string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, tok)
}
