package access_service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// Cifragem dos segredos guardados no banco (senha da conta de serviço do
// LDAP, segredos de 2FA). AES-256-GCM com chave derivada de
// EVOGO_SECRET_KEY — ou, se ela não existir, da GLOBAL_API_KEY.
//
// ⚠️ Sem EVOGO_SECRET_KEY, trocar a GLOBAL_API_KEY torna os segredos
// ilegíveis: será preciso redigitar a senha do LDAP e refazer o 2FA. Por isso
// a recomendação é definir EVOGO_SECRET_KEY (valor longo, guardado no cofre)
// e nunca mais trocá-la.

const encPrefix = "enc:v1:"

var errDecrypt = errors.New("não foi possível decifrar o segredo (EVOGO_SECRET_KEY/GLOBAL_API_KEY mudou?)")

func secretKey(globalApiKey string) []byte {
	base := strings.TrimSpace(os.Getenv("EVOGO_SECRET_KEY"))
	if base == "" {
		base = globalApiKey
	}
	k := sha256.Sum256([]byte("evogo-secrets:" + base))
	return k[:]
}

func encryptSecret(globalApiKey, plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, encPrefix) {
		return plain, nil
	}
	block, err := aes.NewCipher(secretKey(globalApiKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// decryptSecret aceita valor antigo em texto puro (sem prefixo) e o devolve igual.
func decryptSecret(globalApiKey, stored string) (string, error) {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", errDecrypt
	}
	block, err := aes.NewCipher(secretKey(globalApiKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errDecrypt
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errDecrypt
	}
	return string(plain), nil
}

// randomToken devolve n bytes aleatórios em base64url.
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
