# Evolution GO - Changelog

## v0.16.12

### Fixes
- **`POST /send/link` não renderizava o cartão de link (saía texto puro).** Três causas no `sendLinkWithRetry`:
  1. **Miniatura crua e grande.** `JPEGThumbnail` recebia os bytes baixados inteiros (ex.: 104 KB) — o cliente WhatsApp descarta uma miniatura desse tamanho e, sem miniatura válida, não desenha o cartão. Agora a imagem passa por `gerarThumbnail` (JPEG pequeno) antes de virar `JPEGThumbnail`.
  2. **`data:` URI e base64 quebravam.** A imagem era buscada com `http.Get(imgUrl)`, que falha em `data:image/...;base64,...` → miniatura vazia. Agora usa o helper que aceita URL http(s) **e** data URI.
  3. **Scraping sobrescrevia o que o caller mandou.** `fetchLinkMetadata` rodava sempre que havia URL no texto e apagava `imgUrl`/título/descrição informados. Agora só faz scraping quando o caller não mandou nada (título, descrição e imagem todos vazios).
- **Novo campo `thumbnailBase64`** em `/send/link`: bytes JPEG da miniatura já prontos (com ou sem prefixo `data:`). Tem prioridade sobre `imgUrl` e o servidor só repassa — sem rede, sem processar imagem (encolhe apenas se vier > 200 KB, por segurança).
- **`imgUrl` agora aceita data URI** além de URL http(s).
- `PreviewType` mudou de `VIDEO` (fixo, mostrava play em cima da imagem) para `NONE`, adequado a cartão de link/imagem.

### Notes
- Esta versão do whatsmeow **não expõe `CanonicalURL`** no `ExtendedTextMessage` — o cartão depende de `MatchedText` + miniatura válida + título/descrição. Na prática: a **URL precisa aparecer no corpo (`text`)** da mensagem pro WhatsApp atrelar o preview; `url` sozinho (fora do texto) pode não renderizar.
- `/send/link` **não** é bloqueado em `@newsletter` (ao contrário de botão/lista/carrossel) — é justamente o formato clicável e leve recomendado para canais.

## v0.16.11

### Features
- **`POST /send/button` agora aceita imagem no cabeçalho** (`image`: URL http(s) ou data URI `data:image/...;base64,...`). Renderiza **foto + botões numa única mensagem nativa** em conversa/grupo — antes o único jeito de ter foto com botão era carrossel de 1 card. A imagem entra como `Header.Media` (mesmo caminho do card do carrossel), com miniatura JPEG embutida (`gerarThumbnail`). JPEG recomendado.
- **`id` determinístico opcional** em `ButtonStruct` e `CarouselStruct`. Se informado, vira o id da mensagem no WhatsApp (antes era sempre gerado) — permite retry idempotente e consultar depois via `/message/status`. `sendText`/`sendMedia` já tinham; botão e carrossel eram os que faltavam para o fallback seguro.

### Fixes
- **Interativos em canal agora são recusados com motivo.** `/send/button`, `/send/list` e `/send/carousel` para um JID `@newsletter` retornam 400 `"canais do WhatsApp não suportam <tipo>; use texto, mídia ou enquete"`. Antes a API aceitava, o WhatsApp gravava e o leitor via só o placeholder cinza "não é possível carregar, use o celular". Canal é broadcast puro, não renderiza mensagem interativa.

### Notes
- Carrossel Premium multi-card (com critério de preço histórico) segue como atividade futura — não faz parte desta entrega. Esta versão cobre a primitiva técnica: foto + botão numa mensagem, via `/send/button` com `image` (ou carrossel de 1 card, que continua válido).

## v0.16.10

### Improvements
- **`GET /polls/{id}/results` agora devolve o TEXTO das opções, não só o hash.** O voto do WhatsApp só carrega o SHA-256 do texto da opção, então o endpoint respondia `optionCounts: { "<hash>": 4 }` — ilegível. Agora a resposta traz `options[]` com `{ name, hash, count, percentage, known }`, `question`, `totalVoters` e `voters[].selectedOptionNames`. Ordenado por mais votada.
- **Como o texto é recuperado (híbrido):** no envio, `POST /send/poll` passa a gravar as opções (texto + hash) numa tabela nova `poll_options` (criada sozinha no boot, via auto-migração — best-effort, não impede o envio). No results, faz o join hash→texto. Para enquetes enviadas antes desta versão, o chamador pode passar as opções no query — `?options=Sim|Não` ou repetido `?option=Sim&option=Não` — que o backend calcula o mesmo SHA-256 e rotula. As opções do query também sobrepõem/completam o que estiver gravado.
- **Opções com zero voto agora aparecem** (antes sumiam), com `count: 0`. Percentual é sobre o total de votantes.
- `known: false` marca votos para uma opção que não conseguimos rotular (ex.: enquete antiga sem registro e sem `?options`) — o hash ainda vem, então nada se perde.
- O hashing foi confirmado empiricamente contra votos reais em produção: `sha256("Sim")`, `sha256("SIM")` e `sha256("NÃO")` batem com os hashes armazenados (case-sensitive, acento/emoji preservados).

### Fixes
- Removido o `fmt.Printf("[POLL DEBUG] ...")` que imprimia no stdout a cada voto recebido (ruído em produção, 5 linhas por voto).

### Notes
- `totalVotes` foi mantido (== `totalVoters`, uma linha por votante) por compatibilidade; `optionCounts` (hash→count) também continua na resposta.
- Enquete com 0 voto mas com opções conhecidas agora responde **200** (mostra as opções zeradas) em vez de 404. O 404 fica só quando não há nem voto nem opção para exibir.

## v0.16.9

### Fixes
- **Images sent to a channel showed a grey placeholder instead of the picture** — media messages went out with no `JPEGThumbnail`, so WhatsApp drew a download box rather than rendering the image. In a group it went unnoticed because the phone's auto-download fills it in; a channel (`@newsletter`) has no auto-download, so the image simply never appeared. Both `sendMediaFileWithRetry` (file/base64) and `sendMediaUrlWithRetry` (by URL) build their messages independently, and each has a newsletter and a normal variant — all 8 construction points now set the thumbnail, plus 2 in `sendStatusMedia` (status/stories had the same defect, outside the original scope).
- Thumbnail generation is centralised in a new `gerarThumbnail` helper (72px wide, aspect preserved, JPEG quality 50 — measured at 1.2–1.3 KB). The logic already existed inline inside the carousel builder; the carousel now calls the helper, removing the duplicate.
- Failures are swallowed on purpose: a broken/404/truncated image, or any video, returns `nil` and the message is sent without a thumbnail. `JPEGThumbnail` is an optional protobuf field, so that is exactly the previous behaviour — a missing thumbnail is acceptable degradation, a send that fails would be a regression.
- Added a guard the original inline code lacked: a decoded image reporting zero width would make the aspect-ratio division produce `+Inf` and the resulting `int` allocate an absurd buffer.

### Notes
- **Video still has no thumbnail** (returns `nil`): `image.Decode` cannot read an MP4 container. Generating one requires extracting a frame with ffmpeg — deliberately left out so it would not hold up the image fix.
- **WebP works.** Verified empirically that `github.com/chai2010/webp` registers its decoder with `image.Decode` (`format="webp"`), so WebP sources produce thumbnails like JPEG/PNG.
- First test file in the repo: `pkg/sendMessage/service/thumbnail_test.go` covers JPEG/PNG/WebP/1x1/panoramic plus the nil-on-garbage contract (404 HTML, MP4, empty, nil, truncated), so a future refactor cannot turn a decode failure back into a failed send.
- Pre-existing and untouched: `SendLink` passes the full downloaded image as `JPEGThumbnail` instead of a resized one, which can put megabytes in the field. Worth a separate look.

## v0.16.8

### Fixes
- **`POST /send/button` printed the title twice on the phone** — the title was written into two places of the same `InteractiveMessage`: prefixed in bold at the top of the `Body` text (`body := "*" + data.Title + "*"`) *and* set as `Header.Title`. WhatsApp renders the header and the body as separate areas, so both showed up and the message opened with the title repeated on two lines before the description. `Body` now carries only the description; the title stays in the `Header` alone.
- Also fixes a side effect of the old concatenation: with an empty title the body started with a stray `**`, and the trailing `\n` after the description added blank space above the buttons.
- Guard kept for the case where only a title is supplied and no description: the title then becomes the body and the header is dropped, because an `InteractiveMessage` with an empty `Body` is rejected by WhatsApp. `/send/button` already requires `description` at the handler, so this only matters for callers using the service directly.
- `POST /send/list` and `POST /send/carousel` were checked and are **not** affected — they map title/description to their own protobuf fields with no duplication.

## v0.16.7

### Fixes
- **Webhook no longer leaks `@lid` instead of the phone number on messages you sent** — WhatsApp is migrating addressing from phone number (`@s.whatsapp.net`) to LID (`@lid`), an opaque id. The normalization in `myEventHandler` only covered one path: `Sender` is a LID *and* `SenderAlt` carries the number. On an `IsFromMe` message (the echo of a message sent from the phone/another device) `SenderAlt` comes back **empty**, so the condition never matched, the `else` branch just stripped the device suffix, and `Info.Chat`/`Info.Sender` reached the webhook as raw `@lid` — with no phone number anywhere the consumer would look for one. Confirmed against production logs: the old swap fired 2515 times in 6h for received messages (which did work) and never for `IsFromMe`.
- Normalization now resolves the number from three sources, in order: `SenderAlt` (received messages, unchanged behaviour), `RecipientAlt` (only when `IsFromMe` — on a received message `RecipientAlt` is *our own* number, so using it there would be wrong), then the LID↔number mapping whatsmeow keeps locally (`Store.LIDs.GetPNForLID`, local store lookup, no network call to WhatsApp). `Info.Chat` now carries the phone number for both directions, so existing consumers reading `Info.Chat` work without changes.
- Group and newsletter chats are untouched: `Info.Chat` stays the group JID (`@g.us`) and only `Info.Sender` is resolved. The LID is never discarded — it always ends up in `Info.SenderAlt`. When the mapping is genuinely unknown the LID is kept rather than emitting an empty field.

### Improvements
- Replaced the hand-rolled `cleanSenderID` string surgery with whatsmeow's own `JID.ToNonAD()` for stripping the device suffix (`:12@…`), and dropped the now-unused helper.
- The LID path logs one line per message instead of two (it was ~5000 lines / 6h at INFO on a single busy instance).

### CI/CD
- **Image version no longer stamps as `main`** — the build passed `VERSION=${{ github.ref_name }}`, which on a push to the default branch is literally the string `main`, so anything running the `:latest` image reported `Starting Evolution GO version main`. The build arg now comes from the `VERSION` file, so the same number is stamped whether the image was built from a branch push or from a `v*` tag.
- Pushes to `main` now also publish the plain version tag (e.g. `0.16.7`) alongside `latest` and `sha-…`, so a specific build can be pinned without waiting for a release tag.
- The workflow writes a job summary with the published version, commit and the ready-to-use `docker pull` line.
- **New `deploy/docker-compose.selfhost.yml`** — plain Compose (no Swarm) stack for running this project in another environment straight from the published image: `evolution-go` + `postgres` + `watchtower` for auto-update. The GHCR package is public, so the pull needs no `docker login`; only `GLOBAL_API_KEY` and `DB_PASSWORD` have to be supplied via `.env`.

## v0.16.6

### Fixes
- **`POST /group/participant` works again** — `ValidateJIDFields` only handled string fields. For an array field such as `participants`, the `value.(string)` assertion failed and `strValue` got the zero value `""`, which then matched the `else if strValue == ""` branch: every request was rejected with `participants is required and cannot be empty` before the handler ran. No participant format worked (bare number, `@c.us`, `@s.whatsapp.net`) because the content was never read at all. The middleware now uses a type switch with `string` and `[]interface{}` branches plus a `default` that returns an explicit 400 for an invalid type. Array items are validated and normalized individually, matching what `ValidateMultipleNumbers` already did for `/group/create`.
- **`/group/participant` validated a field that does not exist** — the route asked for validation of `number`, but `AddParticipantStruct` uses `groupJid`. Since `number` is never present in the body, validation was a silent no-op and `groupJid` was never normalized. Changed to `ValidateJIDFields("groupJid", "participants")`.

### Known issues (not fixed here)
- The same non-existent-field pattern remains on 7 `/group` routes (`info`, `invitelink`, `photo`, `name`, `description`, `settings`, `leave` — all ask for `number` while their structs use `groupJid`) and on `/community/add` + `/community/remove` (ask for `number`/`communityId` while the struct uses `communityJid`/`groupJid`). JID validation is therefore a no-op on those 9 routes. Deliberately left alone: enabling validation where there is none today can introduce a 400 in flows that currently work, so it needs its own change plus tests.
- This CHANGELOG has no entries for v0.12.0 through v0.16.5. Note that `Makefile` derives its version from `grep -om1 "v[0-9].*" CHANGELOG.md`, so a `make`-driven build stamps the topmost entry here — keep it in sync with the `VERSION` file.

## v0.11.2

### Fixes
- Group metadata parsing now accepts WhatsApp responses that omit `creation` for legacy/community groups, avoiding misleading parse warnings while preserving the group with an unknown creation date.

## v0.11.1

### Improvements
- **Real group activity**: `GET /group/list` now exposes `lastMessageAt`, `lastMessageId` and `lastMessageStatus` from the local message index.
- **Incoming message indexing**: `Message` events are persisted by canonical JID so the latest activity can be identified per group.
- **Legacy compatibility**: activity lookup accepts rows stored without the server suffix in the JID.

### Fixes
- Group listing no longer treats name/description changes as the last message date.
- Groups without indexed activity are not automatically classified as inactive.

## v0.7.1

**Docker:** `evoapicloud/evolution-go:0.7.1`

### 🆕 New Features
- **Test-send modal in Manager** — new modal in the embedded manager UI to test message sending directly from the panel, covering text, media and interactive message types. Useful for validating an instance right after pairing without leaving the manager.

### 🔧 Improvements / CI
- **whatsmeow-lib SHA now pinned in the public sync** — the `sync-releases` workflow previously re-cloned whatsmeow `main` on every run, so the SHA listed in the CHANGELOG could drift from what the public repos actually built against. The workflow now captures the SHA from the dev submodule and checks out that exact commit in the target, restoring release reproducibility.
- **Repository cleanup** — dropped tracked binaries (`evolution-go`, `build/server`), IDE config (`.idea/`) and scratch files (`DIFF-COMPLETO.txt`, `API-INTERACTIVE-DOCS.txt`, `carousel-sender.html`). Expanded `.gitignore` to prevent reincidence.

### 📝 Docs
- **Postman collection** — added `Set Proxy` request and multipart hints on `/send/media`; collection file renamed from `Evolution GO.postman_collection (2).json` to `Evolution GO.postman_collection.json`.
- **Interactive messages docs** — additional examples and corrections.

## v0.7.0

**Docker:** `evoapicloud/evolution-go:0.7.0`

### 🆕 New Features
- **Multi-platform interactive messages** — Buttons, lists and carousel working on Android, iOS and WhatsApp Web/Desktop
  - **SendButton**: removed `ViewOnceMessage` wrapper that blocked rendering on iOS and WhatsApp Web; `Footer` and `Header` are now conditional
  - **SendList**: migrated from `InteractiveMessage`/`NativeFlowMessage` to legacy `ListMessage` (native protobuf) for broad compatibility
  - **SendCarousel**: new endpoint `POST /send/carousel` with cards (image, text, footer, buttons) and automatic JPEG thumbnail generation for instant image loading
  - `whatsmeow-lib`: added `biz` node for `InteractiveMessage` and pinned `product_list` type on the `biz` node for `ListMessage`
- **Base64 media support on `/send/media`** — The `url` field on `POST /send/media` now also accepts base64-encoded media. When the value does not start with `http://` or `https://`, it is treated as base64 and decoded; reuses the existing `SendMediaFile` flow
- **WhatsApp status endpoints** — new `POST /send/status/text` and `POST /send/status/media` publish text/image/video status to `status@broadcast`. Media endpoint supports both JSON (with URL) and multipart/form-data (file upload). Thanks @Eduardo-gato (#15)
- **Webhook routing for GROUP / NEWSLETTER** — when the primary `MESSAGE` / `SEND_MESSAGE` / `READ_RECEIPT` subscription is absent, events from `@g.us` chats are forwarded to `GROUP` subscribers and events from `@newsletter` chats to `NEWSLETTER` subscribers. Thanks @oismaelash (#18)

### 🔧 Improvements
- **Proxy protocol** — new optional `protocol` field (and `PROXY_PROTOCOL` env) supporting `http`, `https`, `socks5`. Replaces the hardcoded SOCKS5 dialer with `client.SetProxyAddress`, fixing HTTP-proxy QR pairing (#12). Thanks @TBDevMaster (#13)
- **WhatsApp Web version cache** — `fetchWhatsAppWebVersion` now caches the result for 1 hour with a mutex instead of issuing one request per instance startup. Thanks @VitorS0uza (#24)
- **Manager flicker fix** — instance page no longer replaces the list with skeleton cards on every 5s polling cycle (`hasLoaded` flag). Thanks @TBDevMaster (#14), closes #11
- **`WEBHOOKFILES` → `WEBHOOK_FILES`** — `.env.example`, docker-compose and docs aligned with the env var the runtime actually reads. Thanks @VitorS0uza (#22)
- **Dependency cleanup** — removed unused `github.com/EvolutionAPI/evo-gate` from `go.mod`
- **whatsmeow-lib** bumped to `0923702fb`
- **Telemetry removed** — dropped legacy `pkg/telemetry`

### 🐛 Bug Fixes
- **`/message/edit`** — was silently ignored because the edit payload used `Conversation` while the original message was sent as `ExtendedTextMessage`. WhatsApp requires matching types; now the edit uses `ExtendedTextMessage` and the response returns the actual server timestamp instead of the zero value. Closes #16
- **Sticker upload to S3/MinIO** — when `webp.Decode` or `png.Encode` failed, the whole media pipeline aborted and the sticker was lost from the webhook. Now we log a warning and keep the raw `.webp` bytes so the sticker still reaches the bucket. Closes #5
- **Multipart `/send/media`** — the binary-upload branch silently dropped `mentionAll`, `mentionedJid` and `quoted`. These fields now parse from the form (with `mentionedJid` accepting repeated or comma-separated values) and reach the send service. Closes #2

### ⚠️ Breaking changes
- **Proxy** — previously all proxies were forced through SOCKS5. If you run SOCKS5 on a non-standard port (anything outside 1080/2080/42000-43000), set `PROXY_PROTOCOL=socks5` in the env or pass `"protocol": "socks5"` in the proxy body explicitly — otherwise the new protocol inference will fall back to HTTP.

### 📝 Docs
- **README** — updated WhatsApp support number and issue templates
- **Interactive messages guide** — new `docs/wiki/guias-api/api-interactive.md`
- **Proxy docs** — environment variables, configuration guide and API reference updated with the new `protocol` field

## v0.6.1

### 🆕 New Features
- **Group invite info endpoint** — `GET /group/invite-info` to get group details from invite link
- **Enhanced media sending** — GIF playback, video stickers, and transparent sticker support

### 🐛 Bug Fixes
- **Admin revoke** — Allow deleting messages from others in groups (admin revoke)

### 🔧 Improvements
- **Version management** — Reads version from `VERSION` file with ldflags fallback
- **CORS global middleware** — Applied before all routes
- **Makefile compatibility** — Fixed `$(shell)` syntax for GNU Make 3.81 (macOS default)
- **CI/CD cleanup** — Removed `develop` branch trigger and `homolog` tag from Docker workflow
- **README updated** — New links, documentation, and hosting info

## v0.6.0

### 🆕 New Features
- **Version from VERSION file** — Reads version from `VERSION` file at startup instead of hardcoded value

### 🔧 Improvements
- **Makefile compatibility** — Fixed `$(shell)` syntax for GNU Make 3.81 (macOS default)

## v0.5.4

### 🔧 Improvements
- **Update whatsmeow lib**

## v0.5.3

**Docker:** `evoapicloud/evolution-go:0.5.3`

### 🔧 Improvements

- **Update context handling in service methods** 
  - Refactored multiple service methods across various packages to include `context.Background()` as the first argument in client calls. This change ensures that all client interactions are properly context-aware, allowing for better cancellation and timeout management.
  - Updated methods in `call_service.go`, `community_service.go`, `group_service.go`, `message_service.go`, `newsletter_service.go`, `send_service.go`, `user_service.go`, and `whatsmeow.go` to enhance consistency and reliability in handling requests.
  - This adjustment improves the overall robustness of the API by ensuring that all client calls can leverage context for better control over execution flow and resource management.

## v0.5.2

**Docker:** `evoapicloud/evolution-go:0.5.2`

### 🆕 New Features
- **SetProxy Endpoint**: New endpoint `POST /instance/proxy/{instanceId}` to configure proxy for instances
  - Support for proxy with/without authentication
  - Validation of required fields (host, port)
  - Automatic cache update via reconnection
  - Integrated Swagger documentation

### 🔧 Improvements
- **CheckUser Fallback Logic**: Implemented intelligent fallback logic
  - If `formatJid=true` returns `IsInWhatsapp=false`, automatically retries with `formatJid=false`
  - Significant improvement in valid user detection
  - Added `RemoteJID` field to use WhatsApp-validated JID
- **LID/WhatsApp JID Swap**: Automatic handling of special cases
  - When `Sender` comes as `@lid` and `SenderAlt` comes as `@s.whatsapp.net`
  - Automatic inversion: `Sender` and `Chat` receive `@s.whatsapp.net`, `SenderAlt` receives `@lid`
  - Detailed logs for tracking swaps

### 🐛 Bug Fixes
- **SendMessage**: Standardization of WhatsApp-validated `remoteJID` usage
- **User Validation**: Improvement in phone number validation and formatting

---

## v0.5.1

**Docker:** `evoapicloud/evolution-go:0.5.1`

### 🔧 Improvements
- **Instance Deletion**: Enhance instance deletion and media storage path resolution
- **Media Storage**: Improvements in media storage and path resolution

---

## v0.5.0

**Docker:** `evoapicloud/evolution-go:0.5.0`

### 🔧 Improvements
- **Media Storage**: Enhance media storage and logging in Whatsmeow event handling
- **Retry Logic**: Implement retry logic for client connection and message sending
- **Media Handling**: Enhance media handling in event processing

---

## v0.4.9

**Docker:** `evoapicloud/evolution-go:0.4.9`

### 🔧 Improvements
- **Connection Handling**: Add instance update test scenarios and improve connection handling
- **FormatJid Field**: Update FormatJid field to pointer type for better handling in message structures
- **Dependencies**: Update dependencies and fix presence handling in Whatsmeow integration

---

## v0.4.8

**Docker:** `evoapicloud/evolution-go:0.4.8`

### 🔧 Improvements
- **Audio Duration**: Improve audio duration parsing in convertAudioToOpusWithDuration function

---

## v0.4.7

**Docker:** `evoapicloud/evolution-go:0.4.7`

### 🔧 Improvements
- **Phone Number Formatting**: Improve phone number formatting and validation in user service
- **Brazilian/Portuguese Numbers**: Update Brazilian and Portuguese number formatting in utils

### 🆕 New Features
- **Media Handling**: Enhance media handling in event processing

---

## v0.4.6

**Docker:** `evoapicloud/evolution-go:0.4.6`

### 🆕 New Features
- **User Existence Check**: Add user existence check configuration and JID validation middleware

---

## v0.4.5

**Docker:** `evoapicloud/evolution-go:0.4.5`

### 🔧 Improvements
- **Dependencies**: Update dependencies and enhance audio conversion functionality

---

## v0.4.4

**Docker:** `evoapicloud/evolution-go:0.4.4`

### 🆕 New Features
- **CLAUDE.md**: Add CLAUDE.md for project documentation and enhance RabbitMQ connection handling

---

## v0.4.3

**Docker:** `evoapicloud/evolution-go:0.4.3`

### 🔧 Improvements
- **PostgreSQL Connection**: Fix in PostgreSQL connection configuration for session auth
  - Controlled configuration of pool, idle, etc.
  - Adjustment on top of whatsmeow lib
- **User Endpoints**: Fix in 'User Info' and 'Check User' endpoints
  - Now return with contact's LID information

---

## v0.3.0

### 🆕 New Features
- **Own Message Reactions**: Additional 'fromMe' parameter using Chat id
- **CreatedAt Field**: CreatedAt field added to instances table

---

## v0.2.0

### 🆕 New Features
- **Advanced Settings**: Advanced configurations in instance creation
  - `alwaysOnline` (still to be implemented)
  - `rejectCall` - Automatically reject calls
  - `msgRejectCall` - Call rejection message
  - `readMessages` - Automatically mark messages as read
  - `ignoreGroups` - Ignore group messages
  - `ignoreStatus` - Ignore status messages
- **Advanced Settings Routes**: New routes for get and update of advanced settings
- **QR Code Control**: `QRCODE_MAX_COUNT` variable to control how many QR codes to generate before timeout
- **AMQP Events**: `AMQP_SPECIFIC_EVENTS` variable to individually select which events to receive in RabbitMQ

### 🔧 Improvements
- **Reconnect Endpoint**: Fix in reconnect endpoint
- **Sender Info**: `Sender` and `SenderAlt` no longer come with session id, only the id

### 🐛 Bug Fixes
- **QR Code Generation**: Fix to not generate QR code automatically after disconnection or logout

---

## v0.1.0

### 🆕 Initial Features
- Base implementation of Evolution API in Go
- WhatsApp integration via whatsmeow
- Instance system
- Basic message sending endpoints
- Webhook support
- RabbitMQ and NATS integration
- Authentication system
- Swagger documentation

---

## 📋 Migration Notes

### v0.5.2
- The new `SetProxy` endpoint requires admin permissions (`AuthAdmin`)
- The `CheckUser` fallback logic is automatic and transparent
- LID/WhatsApp JID handling is automatic

### v0.4.3
- Check PostgreSQL connection settings if using postgres auth

### v0.2.0
- Review advanced settings configurations if necessary
- Configure `QRCODE_MAX_COUNT` if you want to limit QR codes
- Configure `AMQP_SPECIFIC_EVENTS` for specific RabbitMQ events

---

## 🔗 Useful Links

- **Docker Hub**: `evoapicloud/evolution-go`
- **Documentation**: Swagger available at `/swagger/`
- **GitHub**: [Evolution API Go](https://github.com/EvolutionAPI/evolution-go)

---

## 🤝 Contributing

To contribute to the project:
1. Fork the repository
2. Create a branch for your feature
3. Commit your changes
4. Open a Pull Request

---

*Last updated: October 2025*

