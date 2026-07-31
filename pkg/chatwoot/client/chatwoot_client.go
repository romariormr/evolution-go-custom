package chatwoot_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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

	respBody, err := c.doJSON(http.MethodPost, url, token, map[string]any{
		"name": inboxName,
		"channel": map[string]any{
			"type": "api",
		},
	})
	if err != nil {
		return "", err
	}

	var parsed struct {
		Id int `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("resposta inesperada do chatwoot: %w", err)
	}

	return fmt.Sprintf("%d", parsed.Id), nil
}

// retryDelays define o backoff entre tentativas — falha de rede/instabilidade
// pontual do Chatwoot não pode virar mensagem perdida silenciosamente.
var retryDelays = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// isRetryableStatus decide se vale tentar de novo: falha de rede (sem status)
// e 5xx são transitórios; 4xx é erro de validação real (ex.: telefone
// inválido) — repetir não muda o resultado.
func isRetryableStatus(statusCode int) bool {
	return statusCode == 0 || statusCode >= 500
}

func (c *Client) doJSON(method, url, token string, body map[string]any) ([]byte, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelays[attempt-1])
		}

		var reader io.Reader
		if encoded != nil {
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
			lastErr = err
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("chatwoot retornou %d: %s", resp.StatusCode, string(respBody))
			if !isRetryableStatus(resp.StatusCode) {
				return nil, lastErr
			}
			continue
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("falha após %d tentativas: %w", len(retryDelays)+1, lastErr)
}

// FindOrCreateContact garante que existe um contato com esse telefone vinculado à inbox
// (contact_inbox), criando-o se necessário. Retorna o contactId e o sourceId (identidade
// do contato dentro do canal da inbox, usado pra abrir a conversa).
// Doc: POST /api/v1/accounts/{account_id}/contacts
func (c *Client) FindOrCreateContact(baseURL, accountId, token, inboxId, name, phoneNumber, identifier string) (contactId string, sourceId string, err error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts", strings.TrimRight(baseURL, "/"), accountId)

	inboxIdInt, _ := strconv.Atoi(inboxId)
	body := map[string]any{
		"inbox_id":     inboxIdInt,
		"name":         name,
		"phone_number": phoneNumber,
		"identifier":   identifier,
	}

	respBody, err := c.doJSON(http.MethodPost, url, token, body)
	if err != nil {
		// Contato com esse identifier/telefone já pode existir nessa conta —
		// tenta localizar via busca antes de desistir.
		return c.searchContact(baseURL, accountId, token, phoneNumber, inboxId)
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

func (c *Client) searchContact(baseURL, accountId, token, phoneNumber, inboxId string) (contactId string, sourceId string, err error) {
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
			Id             int    `json:"id"`
			PhoneNumber    string `json:"phone_number"`
			ContactInboxes []struct {
				SourceId string `json:"source_id"`
				Inbox    struct {
					Id int `json:"id"`
				} `json:"inbox"`
			} `json:"contact_inboxes"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", "", fmt.Errorf("resposta inesperada do chatwoot ao buscar contato: %w", err)
	}

	// A busca do chatwoot é fuzzy (por nome/email/telefone parcial) — sem checar o
	// telefone exato aqui, um resultado qualquer do texto pesquisado podia ser aceito
	// como se fosse o contato certo (já aconteceu: query malformada casou com um
	// contato real completamente sem relação, e as mensagens de status foram parar
	// na conversa dele).
	for _, found := range parsed.Payload {
		if found.PhoneNumber != phoneNumber {
			continue
		}
		for _, ci := range found.ContactInboxes {
			if fmt.Sprintf("%d", ci.Inbox.Id) == inboxId {
				return fmt.Sprintf("%d", found.Id), ci.SourceId, nil
			}
		}
	}

	return "", "", fmt.Errorf("contato %s (inbox %s) não encontrado no chatwoot após falha ao criar", phoneNumber, inboxId)
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

// SendTextMessage posta uma mensagem de texto numa conversa existente.
// messageType é "outgoing" (nosso sistema/agente falando) ou "incoming"
// (mensagem real vinda do contato do WhatsApp) — Chatwoot exibe e conta
// não-lidas de forma diferente pra cada um.
// Doc: POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
func (c *Client) SendTextMessage(baseURL, accountId, token, conversationId, content, messageType string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%s/messages", strings.TrimRight(baseURL, "/"), accountId, conversationId)

	body := map[string]any{
		"content":      content,
		"message_type": messageType,
		"private":      false,
	}

	_, err := c.doJSON(http.MethodPost, url, token, body)
	return err
}

// SendMediaMessage posta um arquivo (imagem, áudio, vídeo, documento) como anexo
// numa conversa existente, com legenda opcional. messageType é "incoming"
// (mídia real vinda do WhatsApp) ou "outgoing" (QR code, avisos de sistema).
func (c *Client) SendMediaMessage(baseURL, accountId, token, conversationId string, mediaBytes []byte, filename, mimeType, caption, messageType string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%s/messages", strings.TrimRight(baseURL, "/"), accountId, conversationId)

	buildRequest := func() (*http.Request, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		if err := writer.WriteField("content", caption); err != nil {
			return nil, err
		}
		if err := writer.WriteField("message_type", messageType); err != nil {
			return nil, err
		}

		// CreateFormFile sempre manda "application/octet-stream" — sem o
		// Content-Type real (audio/ogg, video/mp4 etc), o Chatwoot classifica o
		// anexo como "file" genérico em vez de renderizar o player de
		// áudio/vídeo/imagem certo. Por isso monta a parte manualmente.
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, filename))
		if mimeType != "" {
			header.Set("Content-Type", mimeType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(mediaBytes); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}

		req, err := http.NewRequest(http.MethodPost, url, &buf)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("api_access_token", token)
		return req, nil
	}

	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelays[attempt-1])
		}

		req, err := buildRequest()
		if err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("chatwoot retornou %d ao enviar mídia: %s", resp.StatusCode, string(respBody))
			if !isRetryableStatus(resp.StatusCode) {
				return lastErr
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("falha após %d tentativas: %w", len(retryDelays)+1, lastErr)
}
