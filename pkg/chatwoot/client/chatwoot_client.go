package chatwoot_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	neturl "net/url"
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
func (c *Client) CreateInbox(baseURL, accountId, token, inboxName, webhookURL string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/inboxes", strings.TrimRight(baseURL, "/"), accountId)

	channel := map[string]any{
		"type": "api",
	}
	// webhook_url na inbox tipo "api" faz o Chatwoot mandar as respostas do agente
	// de volta pro Evolution GO automaticamente (two-way sem config manual).
	if webhookURL != "" {
		channel["webhook_url"] = webhookURL
	}

	respBody, err := c.doJSON(http.MethodPost, url, token, map[string]any{
		"name":    inboxName,
		"channel": channel,
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

// InboxExists checa se a inbox ainda existe na conta do Chatwoot. Serve pra
// self-heal: se a inbox salva na config foi deletada no Chatwoot, o InboxId fica
// órfão e toda criação de contato/conversa nela dá 404 "Resource could not be
// found". Retorna (true, nil) se existe, (false, nil) se 404, e erro em falha de
// rede/outros status (nesse caso o chamador NÃO deve assumir que sumiu).
// Doc: GET /api/v1/accounts/{account_id}/inboxes/{inbox_id}
func (c *Client) InboxExists(baseURL, accountId, token, inboxId string) (bool, error) {
	if inboxId == "" {
		return false, nil
	}
	url := fmt.Sprintf("%s/api/v1/accounts/%s/inboxes/%s", strings.TrimRight(baseURL, "/"), accountId, inboxId)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("api_access_token", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("chatwoot retornou %d ao verificar inbox %s: %s", resp.StatusCode, inboxId, string(body))
	}
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
		"inbox_id":   inboxIdInt,
		"name":       name,
		"identifier": identifier,
	}
	// Grupo não tem telefone: mandar "phone_number": "" faz o Chatwoot recusar o
	// contato. Só envia o campo quando há telefone de verdade (1:1).
	if phoneNumber != "" {
		body["phone_number"] = phoneNumber
	}

	respBody, err := c.doJSON(http.MethodPost, url, token, body)
	if err != nil {
		// Create falhou quase sempre porque o contato JÁ EXISTE nessa conta —
		// tipicamente vinculado a uma inbox antiga (inbox recriada). Localiza o
		// contato e garante o vínculo (contact_inbox) com a inbox ATUAL; sem isso
		// o contato existe mas não tem source_id nessa inbox e a conversa nunca
		// é criada ("não encontrado após falha ao criar").
		cid, sid, ferr := c.searchContact(baseURL, accountId, token, phoneNumber, identifier, inboxId)
		if ferr != nil {
			return "", "", ferr
		}
		if sid != "" {
			return cid, sid, nil
		}
		sid, aerr := c.EnsureContactInbox(baseURL, accountId, token, cid, inboxId)
		if aerr != nil {
			return "", "", aerr
		}
		return cid, sid, nil
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

// searchContact acha um contato já existente na conta pelo telefone (1:1) ou pelo
// identifier (grupo, que não tem telefone). Devolve o contactId mesmo quando o
// contato NÃO está vinculado à inbox atual — nesse caso sourceId vem vazio e o
// chamador cria o vínculo com EnsureContactInbox. Exigir o vínculo aqui era o que
// quebrava tudo depois de recriar a inbox.
func (c *Client) searchContact(baseURL, accountId, token, phoneNumber, identifier, inboxId string) (contactId string, sourceId string, err error) {
	query := phoneNumber
	if query == "" {
		query = identifier
	}
	if query == "" {
		return "", "", fmt.Errorf("sem telefone nem identifier pra buscar contato no chatwoot")
	}
	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/search?q=%s", strings.TrimRight(baseURL, "/"), accountId, neturl.QueryEscape(query))

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
			Identifier     string `json:"identifier"`
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
		// Casa de forma exata: telefone quando há telefone (1:1), senão identifier
		// (grupo). A busca do Chatwoot é fuzzy — sem o match exato um contato
		// qualquer poderia ser aceito (já aconteceu com as mensagens de status).
		if phoneNumber != "" {
			if found.PhoneNumber != phoneNumber {
				continue
			}
		} else if found.Identifier != identifier {
			continue
		}

		// Achou o contato. Se já tem vínculo com a inbox atual, devolve o source_id;
		// senão devolve só o contactId e o chamador cria o vínculo.
		for _, ci := range found.ContactInboxes {
			if fmt.Sprintf("%d", ci.Inbox.Id) == inboxId {
				return fmt.Sprintf("%d", found.Id), ci.SourceId, nil
			}
		}
		return fmt.Sprintf("%d", found.Id), "", nil
	}

	return "", "", fmt.Errorf("contato %q não encontrado no chatwoot após falha ao criar", query)
}

// EnsureContactInbox vincula um contato já existente à inbox informada e devolve
// o source_id desse vínculo — necessário pra abrir conversa nessa inbox. É o que
// permite reaproveitar contatos antigos quando a inbox é recriada.
// Doc: POST /api/v1/accounts/{account_id}/contacts/{contact_id}/contact_inboxes
func (c *Client) EnsureContactInbox(baseURL, accountId, token, contactId, inboxId string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/%s/contact_inboxes", strings.TrimRight(baseURL, "/"), accountId, contactId)

	inboxIdInt, _ := strconv.Atoi(inboxId)
	respBody, err := c.doJSON(http.MethodPost, url, token, map[string]any{"inbox_id": inboxIdInt})
	if err != nil {
		return "", fmt.Errorf("falha ao vincular contato %s à inbox %s: %w", contactId, inboxId, err)
	}

	// A resposta traz o contact_inbox criado (source_id no topo ou sob payload).
	var parsed struct {
		SourceId string `json:"source_id"`
		Payload  struct {
			SourceId string `json:"source_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("resposta inesperada do chatwoot ao vincular contato à inbox: %w", err)
	}
	if parsed.SourceId != "" {
		return parsed.SourceId, nil
	}
	if parsed.Payload.SourceId != "" {
		return parsed.Payload.SourceId, nil
	}
	return "", fmt.Errorf("chatwoot não devolveu source_id ao vincular contato %s à inbox %s", contactId, inboxId)
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
// sourceId, quando preenchido, marca a mensagem na origem (usamos "WAID:<id da
// mensagem no WhatsApp>"). Serve pra quebrar loop: o webhook do Chatwoot ignora
// mensagens que já vieram do WhatsApp, evitando reenviar de volta pro contato.
func (c *Client) SendTextMessage(baseURL, accountId, token, conversationId, content, messageType, sourceId string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%s/messages", strings.TrimRight(baseURL, "/"), accountId, conversationId)

	body := map[string]any{
		"content":      content,
		"message_type": messageType,
		"private":      false,
	}
	if sourceId != "" {
		body["source_id"] = sourceId
	}

	_, err := c.doJSON(http.MethodPost, url, token, body)
	return err
}

// SendMediaMessage posta um arquivo (imagem, áudio, vídeo, documento) como anexo
// numa conversa existente, com legenda opcional. messageType é "incoming"
// (mídia real vinda do WhatsApp) ou "outgoing" (QR code, avisos de sistema).
func (c *Client) SendMediaMessage(baseURL, accountId, token, conversationId string, mediaBytes []byte, filename, mimeType, caption, messageType, sourceId string) error {
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
		// Mesmo marcador anti-loop do SendTextMessage (ver comentário lá).
		if sourceId != "" {
			if err := writer.WriteField("source_id", sourceId); err != nil {
				return nil, err
			}
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
