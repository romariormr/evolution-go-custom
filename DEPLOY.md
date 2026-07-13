# Deploy — Evolution GO Custom (Portainer / Docker Swarm / Traefik)

Fork de [NathanAshford/evolution-go-custom](https://github.com/NathanAshford/evolution-go-custom)
(que por sua vez é fork do [evolution-foundation/evolution-go](https://github.com/evolution-foundation/evolution-go), base 0.7.1),
**corrigido para compilar** e empacotado para deploy via **Portainer** em Docker Swarm com **Traefik**.

## O que estava quebrado no fork original

O `.gitignore` do fork original tinha estas linhas (para ignorar binários compilados):

```gitignore
evolution-go
server
```

Sem a âncora `/`, o git ignora **qualquer caminho** com esses nomes — o que excluiu do repositório
os diretórios `cmd/evolution-go/` (o `main.go` da aplicação!) e `pkg/server/`. Resultado: o repo
publicado **não compila** (`stat /build/cmd/evolution-go: directory not found`).

## Correções deste fork

1. **`.gitignore`**: linhas ancoradas (`/evolution-go`, `/server`) — os diretórios de código voltaram a ser versionados
2. **`cmd/evolution-go/main.go`**: restaurado do upstream oficial tag `0.7.1` (mesma versão base do fork, ver arquivo `VERSION`)
3. **`pkg/server/handler/server_handler.go`**: restaurado do upstream oficial tag `0.7.1`
4. **`deploy/portainer-stack.yml`**: stack Swarm com PostgreSQL externo + labels Traefik
5. **`deploy/portainer-stack-with-postgres.yml`**: stack Swarm com PostgreSQL incluso + job de init dos bancos

## Build da imagem

No host Docker (primeiro build compila Go + frontend React, ~5 min):

```bash
git clone https://github.com/romariormr/evolution-go-custom.git
cd evolution-go-custom
docker build -t evolution-go-custom:local .
```

## Deploy via Portainer (Swarm)

1. Build da imagem no host (acima)
2. Portainer → **Stacks → Add stack → Web editor**
3. Colar o conteúdo de `deploy/portainer-stack.yml` (DB externo) **ou** `deploy/portainer-stack-with-postgres.yml` (DB incluso)
4. Preencher as **Environment variables** (documentadas no topo de cada YML)
5. Deploy

Ou via API do Portainer:

```bash
JWT=$(curl -sk -X POST https://SEU-PORTAINER/api/auth \
  -d '{"username":"admin","password":"..."}' | jq -r .jwt)

SWARM_ID=$(curl -sk https://SEU-PORTAINER/api/endpoints/1/docker/swarm \
  -H "Authorization: Bearer $JWT" | jq -r .ID)

curl -sk -X POST "https://SEU-PORTAINER/api/stacks/create/swarm/string?endpointId=1" \
  -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
  -d "{
    \"name\": \"evolution-go\",
    \"swarmID\": \"$SWARM_ID\",
    \"stackFileContent\": $(jq -Rs . < deploy/portainer-stack.yml),
    \"env\": [
      {\"name\": \"GLOBAL_API_KEY\", \"value\": \"$(uuidgen)\"},
      {\"name\": \"EVOGO_DOMAIN\", \"value\": \"evolutiongo.exemplo.com\"},
      {\"name\": \"DB_HOST\", \"value\": \"10.0.0.10\"},
      {\"name\": \"DB_USER\", \"value\": \"evogo\"},
      {\"name\": \"DB_PASSWORD\", \"value\": \"...\"}
    ]
  }"
```

### Banco externo — criar usuário e bancos

```sql
CREATE ROLE evogo LOGIN PASSWORD 'senha-forte';
CREATE DATABASE evogo_auth OWNER evogo;
CREATE DATABASE evogo_users OWNER evogo;
```

## Pós-deploy

- Manager: `https://EVOGO_DOMAIN/manager` (login com a `GLOBAL_API_KEY`)
- Swagger: `https://EVOGO_DOMAIN/swagger/index.html`
- Criar instância no Manager → escanear QR code → status `open`

### Webhook por instância (ex.: n8n)

```bash
curl -X POST https://EVOGO_DOMAIN/instance/connect \
  -H "apikey: TOKEN-DA-INSTANCIA" -H "Content-Type: application/json" \
  -d '{"webhookUrl": "https://SEU-N8N/webhook/meu-fluxo", "subscribe": ["MESSAGE", "BUTTON_CLICK"]}'
```

## Integração n8n

Node dedicado (mensagens, botões, listas, carrossel, grupos, instâncias):
**[n8n-nodes-evolutiongo](https://github.com/romariormr/n8n-nodes-evolutiongo)** — `npm install n8n-nodes-evolutiongo`
ou instalar pela UI do n8n em *Settings → Community Nodes*.

## Bugs conhecidos (herdados do código base)

- Evento `ButtonClick` com `type: template_button_reply` chega **sem** os campos `phone`/`jid`/`chat`.
  Workaround no fluxo: fallback para o número conhecido ou usar o evento `Message` para capturar o remetente.
- O endpoint `GET /instance/all` responde `{"data": [...]}` (objeto com wrapper), não array puro.
