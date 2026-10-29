// Package safefetch baixa URLs informadas por quem chama a API (mídia por URL,
// foto de grupo/perfil, prévia de link, anexos do Chatwoot) com proteção contra
// SSRF: sem isso, quem tem uma apikey de instância consegue fazer o servidor
// buscar endereços internos (MinIO, Portainer, metadados de nuvem) e devolver
// o conteúdo como "mídia".
//
// Regras:
//   - Só http/https.
//   - Sempre bloqueado: loopback (127.0.0.0/8, ::1), link-local (169.254.0.0/16
//     — inclui o endereço de metadados de nuvem — e fe80::/10), 0.0.0.0/::,
//     multicast.
//   - Redes privadas (10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7):
//     liberadas por padrão (n8n/MinIO internos mandam mídia por URL interna),
//     bloqueadas com MEDIA_FETCH_BLOCK_PRIVATE=true. Cada acesso a IP privado
//     é registrado no log, para montar a lista de exceções antes de bloquear.
//   - MEDIA_FETCH_ALLOWED_HOSTS: exceções (host, host:porta ou CIDR,
//     separados por vírgula) liberadas mesmo com o bloqueio ligado.
//   - O IP é conferido na hora de conectar (vale também para redirecionamentos
//     e impede troca de DNS entre a checagem e a conexão).
//   - Limites: MEDIA_FETCH_TIMEOUT_SEC (padrão 120) e MEDIA_FETCH_MAX_MB
//     (padrão 100) — corpo maior que isso dá erro em vez de lotar a memória.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrBlocked  = errors.New("endereço bloqueado por segurança (rede interna)")
	ErrTooLarge = errors.New("arquivo maior que o limite permitido")
)

type policy struct {
	blockPrivate bool
	allowHosts   map[string]bool
	allowNets    []*net.IPNet
	maxBytes     int64
	timeout      time.Duration
}

var (
	pol     *policy
	polOnce sync.Once
	client  *http.Client
	logf    = log.Printf
)

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return def
}

func loadPolicy() *policy {
	p := &policy{
		blockPrivate: strings.EqualFold(strings.TrimSpace(os.Getenv("MEDIA_FETCH_BLOCK_PRIVATE")), "true"),
		allowHosts:   map[string]bool{},
		maxBytes:     int64(envInt("MEDIA_FETCH_MAX_MB", 100)) << 20,
		timeout:      time.Duration(envInt("MEDIA_FETCH_TIMEOUT_SEC", 120)) * time.Second,
	}
	for _, item := range strings.Split(os.Getenv("MEDIA_FETCH_ALLOWED_HOSTS"), ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(item); err == nil {
			p.allowNets = append(p.allowNets, n)
			continue
		}
		p.allowHosts[item] = true
	}
	return p
}

func getPolicy() *policy {
	polOnce.Do(func() {
		pol = loadPolicy()
		client = newClient(pol)
	})
	return pol
}

// alwaysBlocked: endereços que nunca fazem sentido como origem de mídia.
func alwaysBlocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isPrivate(ip net.IP) bool {
	return ip.IsPrivate() || cgnat.Contains(ip)
}

func (p *policy) hostAllowed(host, hostPort string) bool {
	return p.allowHosts[strings.ToLower(host)] || p.allowHosts[strings.ToLower(hostPort)]
}

func (p *policy) ipAllowedByNet(ip net.IP) bool {
	for _, n := range p.allowNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// checkIP decide se o IP resolvido pode ser acessado.
func (p *policy) checkIP(host string, ip net.IP) error {
	if p.ipAllowedByNet(ip) {
		return nil
	}
	if alwaysBlocked(ip) {
		return fmt.Errorf("%w: %s (%s)", ErrBlocked, host, ip)
	}
	if isPrivate(ip) {
		if p.blockPrivate {
			return fmt.Errorf("%w: %s (%s)", ErrBlocked, host, ip)
		}
		logf("[SAFEFETCH] acesso a endereço interno liberado: %s (%s) — para bloquear, MEDIA_FETCH_BLOCK_PRIVATE=true e este host em MEDIA_FETCH_ALLOWED_HOSTS", host, ip)
	}
	return nil
}

func newClient(p *policy) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil, // proxy pularia a checagem de IP
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if p.hostAllowed(host, address) {
				return dialer.DialContext(ctx, network, address)
			}
			var ips []net.IP
			if ip := net.ParseIP(host); ip != nil {
				ips = []net.IP{ip}
			} else {
				addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, a := range addrs {
					ips = append(ips, a.IP)
				}
			}
			var lastErr error = fmt.Errorf("sem endereço para %s", host)
			for _, ip := range ips {
				if err := p.checkIP(host, ip); err != nil {
					lastErr = err
					continue
				}
				// conecta no IP já conferido (não resolve de novo)
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Timeout:   p.timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("redirecionamentos demais")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirecionamento para esquema não permitido: %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// Client devolve o cliente HTTP protegido (para quem precisa montar o request).
// Respostas obtidas por ele não têm o corpo limitado — use LimitBody.
func Client() *http.Client {
	getPolicy()
	return client
}

func validate(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("URL inválida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL deve começar com http:// ou https://")
	}
	if u.Hostname() == "" {
		return errors.New("URL sem host")
	}
	return nil
}

type limitedBody struct {
	r     io.Reader
	c     io.Closer
	left  int64
	limit int64
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.left <= 0 {
		// lê 1 byte a mais para saber se passou do limite ou terminou certinho
		var one [1]byte
		n, _ := l.r.Read(one[:])
		if n > 0 {
			return 0, fmt.Errorf("%w (%d MB)", ErrTooLarge, l.limit>>20)
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

func (l *limitedBody) Close() error { return l.c.Close() }

// LimitBody limita o corpo da resposta ao tamanho máximo configurado.
func LimitBody(resp *http.Response) {
	p := getPolicy()
	resp.Body = &limitedBody{r: resp.Body, c: resp.Body, left: p.maxBytes, limit: p.maxBytes}
}

// Get faz um GET protegido; o corpo da resposta já vem limitado.
func Get(rawURL string) (*http.Response, error) {
	if err := validate(rawURL); err != nil {
		return nil, err
	}
	resp, err := Client().Get(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	LimitBody(resp)
	return resp, nil
}

// Fetch baixa o conteúdo inteiro (exige status 2xx).
func Fetch(rawURL string) ([]byte, error) {
	resp, err := Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("download falhou: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
