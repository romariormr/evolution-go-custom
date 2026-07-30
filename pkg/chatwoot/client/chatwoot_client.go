package chatwoot_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
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

func (c *Client) doJSON(method, url, token string, body map[string]any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chatwoot retornou %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// FindOrCreateContact garante que existe um contato com esse telefone vinculado à inbox
// (contact_inbox), criando-o se necessário. Retorna o contactId e o sourceId (identidade
// do contato dentro do canal da inbox, usado pra abrir a conversa).
// Doc: POST /api/v1/accounts/{account_id}/contacts
func (c *Client) FindOrCreateContact(baseURL, accountId, token, inboxId, name, phoneNumber string) (contactId string, sourceId string, err error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts", strings.TrimRight(baseURL, "/"), accountId)

	inboxIdInt, _ := strconv.Atoi(inboxId)
	body := map[string]any{
		"inbox_id":     inboxIdInt,
		"name":         name,
		"phone_number": phoneNumber,
		"identifier":   fmt.Sprintf("evogo-qr-%s", inboxId),
	}

	respBody, err := c.doJSON(http.MethodPost, url, token, body)
	if err != nil {
		// Contato com esse identifier/telefone já pode existir nessa conta —
		// tenta localizar via busca antes de desistir.
		return c.searchContact(baseURL, accountId, token, phoneNumber)
	}

	var parsed struct {
		Payload struct {
			Contact struct {
				Id int `json:"id"`
			} `json:"contact"`
			ContactInbox struct {
				SourceId string `json:"source_id"`
			} `json:"contact_inbox"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", fmt.Errorf("resposta inesperada do chatwoot ao criar contato: %w", err)
	}

	return fmt.Sprintf("%d", parsed.Payload.Contact.Id), parsed.Payload.ContactInbox.SourceId, nil
}

func (c *Client) searchContact(baseURL, accountId, token, phoneNumber string) (contactId string, sourceId string, err error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/search?q=%s", strings.TrimRight(baseURL, "/"), accountId, phoneNumber)

	req, reqErr := http.NewRequest(http.MethodGet, url, nil)
	if reqErr != nil {
		return "", "", reqErr
	}
	req.Header.Set("api_access_token", token)

	resp, doErr := c.httpClient.Do(req)
	if doErr != nil {
		return "", "", doErr
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("chatwoot retornou %d ao buscar contato: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Payload []struct {
			Id             int `json:"id"`
			ContactInboxes []struct {
				SourceId string `json:"source_id"`
			} `json:"contact_inboxes"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", fmt.Errorf("resposta inesperada do chatwoot ao buscar contato: %w", err)
	}
	if len(parsed.Payload) == 0 {
		return "", "", fmt.Errorf("contato %s não encontrado no chatwoot após falha ao criar", phoneNumber)
	}

	found := parsed.Payload[0]
	src := ""
	if len(found.ContactInboxes) > 0 {
		src = found.ContactInboxes[0].SourceId
	}
	return fmt.Sprintf("%d", found.Id), src, nil
}

// CreateConversation abre uma conversa nova pro contato dentro da inbox.
// Doc: POST /api/v1/accounts/{account_id}/conversations
func (c *Client) CreateConversation(baseURL, accountId, token, inboxId, sourceId, contactId string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations", strings.TrimRight(baseURL, "/"), accountId)

	inboxIdInt, _ := strconv.Atoi(inboxId)
	contactIdInt, _ := strconv.Atoi(contactId)
	body := map[string]any{
		"source_id":  sourceId,
		"inbox_id":   inboxIdInt,
		"contact_id": contactIdInt,
		"status":     "open",
	}

	respBody, err := c.doJSON(http.MethodPost, url, token, body)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Id int `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("resposta inesperada do chatwoot ao criar conversa: %w", err)
	}

	return fmt.Sprintf("%d", parsed.Id), nil
}

// SendTextMessage posta uma mensagem de texto (saída do sistema) numa conversa existente.
// Doc: POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
func (c *Client) SendTextMessage(baseURL, accountId, token, conversationId, content string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%s/messages", strings.TrimRight(baseURL, "/"), accountId, conversationId)

	body := map[string]any{
		"content":      content,
		"message_type": "outgoing",
		"private":      false,
	}

	_, err := c.doJSON(http.MethodPost, url, token, body)
	return err
}

// SendImageMessage posta uma imagem (o QR code) como anexo numa conversa existente.
func (c *Client) SendImageMessage(baseURL, accountId, token, conversationId string, imageBytes []byte, filename, caption string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%s/messages", strings.TrimRight(baseURL, "/"), accountId, conversationId)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("content", caption); err != nil {
		return err
	}
	if err := writer.WriteField("message_type", "outgoing"); err != nil {
		return err
	}

	part, err := writer.CreateFormFile("attachments[]", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(imageBytes); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chatwoot retornou %d ao enviar imagem: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
