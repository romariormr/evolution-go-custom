package chatwoot_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client fala com a API administrativa do Chatwoot (criação de inbox, etc).
// Não confundir com o webhook do Chatwoot (esse é recebido, não chamado por aqui).
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// CreateInbox cria uma inbox do tipo "API Channel" na conta informada, usada pra
// receber/enviar mensagens da instância WhatsApp via API própria (sem canal oficial
// da Meta). Retorna o InboxId criado.
// Doc: POST /api/v1/accounts/{account_id}/inboxes (Channel::Api)
func (c *Client) CreateInbox(baseURL, accountId, token, inboxName string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/inboxes", strings.TrimRight(baseURL, "/"), accountId)

	body, err := json.Marshal(map[string]any{
		"name": inboxName,
		"channel": map[string]any{
			"type": "api",
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("chatwoot retornou %d ao criar inbox: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Id int `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("resposta inesperada do chatwoot: %w", err)
	}

	return fmt.Sprintf("%d", parsed.Id), nil
}
