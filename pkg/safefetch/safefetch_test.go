package safefetch

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func usePolicy(t *testing.T, blockPrivate bool, allow string, maxMB int) {
	t.Helper()
	t.Setenv("MEDIA_FETCH_BLOCK_PRIVATE", map[bool]string{true: "true", false: ""}[blockPrivate])
	t.Setenv("MEDIA_FETCH_ALLOWED_HOSTS", allow)
	if maxMB > 0 {
		t.Setenv("MEDIA_FETCH_MAX_MB", "1")
	} else {
		t.Setenv("MEDIA_FETCH_MAX_MB", "")
	}
	polOnce.Do(func() {}) // garante que o Once não sobrescreve depois
	pol = loadPolicy()
	client = newClient(pol)
}

func TestLoopbackBlockedByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("segredo")) }))
	defer srv.Close()
	usePolicy(t, false, "", 0)
	if _, err := Fetch(srv.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback deveria ser bloqueado, veio %v", err)
	}
}

func TestAllowListOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	usePolicy(t, true, "127.0.0.0/8", 0)
	b, err := Fetch(srv.URL)
	if err != nil || string(b) != "ok" {
		t.Fatalf("CIDR liberado deveria passar: %q %v", b, err)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	usePolicy(t, true, host, 0)
	if _, err := Fetch(srv.URL); err != nil {
		t.Fatalf("host:porta liberado deveria passar: %v", err)
	}
}

func TestRedirectToBlockedHost(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("interno")) }))
	defer internal.Close()
	// o "público" é liberado por host:porta; o redirecionamento leva a outro
	// endereço de loopback que não está na lista.
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer public.Close()
	usePolicy(t, false, strings.TrimPrefix(public.URL, "http://"), 0)
	if _, err := Fetch(public.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("redirecionamento para loopback deveria ser bloqueado, veio %v", err)
	}
}

func TestSizeLimit(t *testing.T) {
	big := strings.Repeat("x", (1<<20)+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(big)) }))
	defer srv.Close()
	usePolicy(t, false, "127.0.0.0/8", 1)
	if _, err := Fetch(srv.URL); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("esperava ErrTooLarge, veio %v", err)
	}
}

func TestSchemeAndClassification(t *testing.T) {
	usePolicy(t, false, "", 0)
	for _, u := range []string{"file:///etc/passwd", "ftp://x/y", "gopher://x", "http://"} {
		if _, err := Get(u); err == nil {
			t.Errorf("%s deveria ser recusada", u)
		}
	}
	p := &policy{blockPrivate: true}
	cases := map[string]bool{ // true = bloqueado
		"169.254.169.254": true, "127.0.0.1": true, "10.1.2.3": true, "172.16.210.58": true,
		"192.168.0.1": true, "100.64.1.1": true, "8.8.8.8": false, "::1": true, "fd00::1": true,
	}
	for ipStr, blocked := range cases {
		err := p.checkIP("h", net.ParseIP(ipStr))
		if (err != nil) != blocked {
			t.Errorf("%s: bloqueado=%v, esperado %v", ipStr, err != nil, blocked)
		}
	}
	p.blockPrivate = false
	if p.checkIP("h", net.ParseIP("10.1.2.3")) != nil {
		t.Error("rede privada deve passar com o bloqueio desligado")
	}
	if p.checkIP("h", net.ParseIP("169.254.169.254")) == nil {
		t.Error("metadados de nuvem devem ser sempre bloqueados")
	}
}
