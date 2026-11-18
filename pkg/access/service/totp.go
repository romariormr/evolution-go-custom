package access_service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TOTP (RFC 6238): o mesmo código de 6 dígitos de Google Authenticator,
// Microsoft Authenticator, Authy etc. Passo de 30s, SHA-1, aceita 1 passo de
// tolerância de relógio para cada lado e não aceita o mesmo código duas vezes.

const (
	totpDigits = 6
	totpPeriod = 30
)

var totpB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTotpSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return totpB32.EncodeToString(b)
}

func totpURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func totpCode(secret string, step int64) (string, error) {
	key, err := totpB32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1000000), nil
}

// lastTotpStep: último passo aceito por usuário, para não aceitar o mesmo código de novo.
var (
	lastTotpMu   sync.Mutex
	lastTotpStep = map[string]int64{}
)

// verifyTotp confere o código no instante "now" (±1 passo).
func verifyTotp(userKey, secret, code string, now time.Time) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != totpDigits {
		return false
	}
	cur := now.Unix() / totpPeriod
	for _, step := range []int64{cur, cur - 1, cur + 1} {
		want, err := totpCode(secret, step)
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			lastTotpMu.Lock()
			defer lastTotpMu.Unlock()
			if userKey != "" && lastTotpStep[userKey] >= step {
				return false
			}
			if userKey != "" {
				lastTotpStep[userKey] = step
			}
			return true
		}
	}
	return false
}
