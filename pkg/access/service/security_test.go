package access_service

import (
	"strings"
	"testing"
	"time"
)

func TestTotpRFC6238(t *testing.T) {
	// Vetor do RFC 6238 (SHA-1, segredo "12345678901234567890"), T=59s → 94287082 (8 dígitos) → 287082 (6).
	secret := totpB32.EncodeToString([]byte("12345678901234567890"))
	code, err := totpCode(secret, 59/totpPeriod)
	if err != nil || code != "287082" {
		t.Fatalf("código esperado 287082, veio %q (%v)", code, err)
	}
}

func TestVerifyTotpWindowAndReplay(t *testing.T) {
	secret := newTotpSecret()
	now := time.Unix(1_800_000_000, 0)
	prev, _ := totpCode(secret, now.Unix()/totpPeriod-1)
	if !verifyTotp("u1", secret, prev, now) {
		t.Fatal("código do passo anterior deveria valer (tolerância de relógio)")
	}
	if verifyTotp("u1", secret, prev, now) {
		t.Fatal("o mesmo código não pode ser aceito duas vezes")
	}
	old, _ := totpCode(secret, now.Unix()/totpPeriod-3)
	if verifyTotp("u2", secret, old, now) {
		t.Fatal("código de 90s atrás não pode valer")
	}
	if verifyTotp("u3", secret, "12345", now) {
		t.Fatal("código com tamanho errado não pode valer")
	}
}

func TestEncryptSecret(t *testing.T) {
	t.Setenv("EVOGO_SECRET_KEY", "")
	enc, err := encryptSecret("chave-a", "senha-do-ldap")
	if err != nil || !strings.HasPrefix(enc, encPrefix) || strings.Contains(enc, "senha") {
		t.Fatalf("cifragem errada: %q %v", enc, err)
	}
	if plain, err := decryptSecret("chave-a", enc); err != nil || plain != "senha-do-ldap" {
		t.Fatalf("decifrar falhou: %q %v", plain, err)
	}
	if _, err := decryptSecret("chave-b", enc); err == nil {
		t.Fatal("chave diferente não pode decifrar")
	}
	if plain, _ := decryptSecret("x", "texto-antigo"); plain != "texto-antigo" {
		t.Fatal("valor antigo sem prefixo deve passar direto")
	}
	t.Setenv("EVOGO_SECRET_KEY", "propria")
	enc2, _ := encryptSecret("chave-a", "s")
	if plain, err := decryptSecret("outra-global", enc2); err != nil || plain != "s" {
		t.Fatal("com EVOGO_SECRET_KEY, trocar a GLOBAL_API_KEY não pode afetar os segredos")
	}
}

func TestLoginGuard(t *testing.T) {
	g := newLoginGuard()
	now := time.Now()
	for i := 0; i < loginMaxFailures; i++ {
		if !g.locked("ana", "1.1.1.1", now).IsZero() {
			t.Fatalf("bloqueou cedo demais (tentativa %d)", i)
		}
		g.fail("ana", "1.1.1.1", now)
	}
	if g.locked("ana", "1.1.1.1", now).IsZero() {
		t.Fatal("deveria bloquear depois de 5 erros")
	}
	if !g.locked("ana", "2.2.2.2", now).IsZero() {
		t.Fatal("outro IP não deveria estar bloqueado ainda")
	}
	if !g.locked("ana", "1.1.1.1", now.Add(loginLockFor+time.Second)).IsZero() {
		t.Fatal("bloqueio deveria expirar")
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]bool{
		"curta1":       false,
		"semnumeroaqui": false,
		"12345678":     false,
		"Boa-senha-9":  true,
		"maria2026x":   true,
	}
	for pw, ok := range cases {
		if err := validatePassword(pw, "joao", "joao@x.com"); (err == nil) != ok {
			t.Errorf("%q: esperado ok=%v, erro=%v", pw, ok, err)
		}
	}
	if validatePassword("joao1234", "joao1234", "") == nil {
		t.Error("senha igual ao usuário deveria ser recusada")
	}
}

func TestTotpURL(t *testing.T) {
	u := totpURL("Evolution GO", "ana@x.com", "ABC")
	if !strings.HasPrefix(u, "otpauth://totp/") || !strings.Contains(u, "secret=ABC") || !strings.Contains(u, "issuer=Evolution+GO") {
		t.Fatalf("URL inesperada: %s", u)
	}
}
