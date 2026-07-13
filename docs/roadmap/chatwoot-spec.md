# Spec — Integração Nativa com Chatwoot

> Status: PLANEJADO. Ainda não iniciado.
> Referência visual do que se busca: painel de configuração nativo por instância
> (URL, Account ID, Token, toggles de comportamento) + import de contatos/mensagens,
> no estilo do Evolution API original.

## Objetivo

Cada instância WhatsApp do Evolution GO poder ser conectada a uma **inbox do Chatwoot**,
com sincronização bidirecional de conversas — sem depender de um relay externo (n8n).

## Por que não é trivial

1. **Chatwoot precisa de um inbox por instância** (API Channel) — criado automaticamente
   ou manualmente, com webhook configurado nos dois sentidos.
2. **Mapeamento contato ↔ conversa**: cada número de WhatsApp que fala com a instância
   precisa virar um Contact + Conversation no Chatwoot (criar se não existir, reusar se já existir).
3. **Fluxo de saída** (agente responde no Chatwoot → grupo New): webhook do Chatwoot →
   Evolution GO → `/send/text` (ou mídia) pro número certo.
4. **Fluxo de entrada** (contato manda mensagem no WhatsApp → aparece no Chatwoot): hoje
   o evento `Message` do WhatsApp **não persiste nada no banco** (ver limitação abaixo) —
   é só repassado via webhook/RabbitMQ/WS. Dá pra plugar o envio pro Chatwoot direto nesse
   ponto, sem precisar de tabela — mas cuidado com esse trecho, é o código de maior
   tráfego do sistema (todo evento de todas as instâncias passa por ali).
5. **Import de contatos/histórico** (como no print do Evolution API: toggles
   "Import Contacts"/"Import Messages" ao conectar QR code) — precisa ler o histórico do
   `whatsmeow` (biblioteca WhatsApp) no momento da conexão, não é dado que já temos hoje.

## ⚠️ Limitação descoberta (pré-requisito compartilhado)

A tabela `messages` do evolution-go **só grava recibo de leitura** (`*events.Receipt`),
nunca o conteúdo real de uma mensagem recebida/enviada (`*events.Message` não persiste
nada). Isso significa:

- Não existe hoje nenhum "log de mensagens" no banco pra importar/exibir.
- O contador "Mensagens" do Dashboard (`docs/roadmap/multi-tenant-spec.md`... na verdade
  ver histórico do projeto) foi **deliberadamente não implementado** por esse motivo.
- Se a Fase 1 do Chatwoot (webhook direto, sem tabela) for suficiente, essa limitação não
  bloqueia nada. Só importa se quisermos "importar histórico de X dias" — aí precisa
  decidir: gravar mensagens no banco (mudança no pipeline principal) OU buscar o
  histórico direto do `whatsmeow` on-demand no momento da conexão (mais alinhado ao
  padrão "Import Messages" do Evolution API, não exige mudar o pipeline de eventos).

## Modelo de dados proposto

Reusa `evogo_settings` (chave/valor, já existe) para configuração **global** (URL base do
Chatwoot, token de admin da conta) — mas cada **instância** precisa da própria config
(account/inbox diferentes por cliente). Duas opções:

- **(A) Nova tabela** `evogo_chatwoot_configs` (instanceId FK, accountId, inboxId, token,
  signMsg, nameInbox, importContacts, importMessages, daysLimitImportMessages, autoCreate,
  ignoreJids) — granularidade por instância, mais flexível pra multi-tenant.
- **(B) Reusar** `AdvancedSettings` do `instance_model.Instance` (adicionar campos) — mais
  simples, mas mistura configuração de canal externo com config nativa do WhatsApp.

Recomendo **(A)** — mais alinhado ao multi-tenant (grupo dono da instância pode ter
Chatwoot próprio) e não poluí o model de Instance.

## Fases propostas

1. **Config + criação de inbox**: modelo `(A)`, endpoints CRUD (`/instance/:id/chatwoot`),
   função que cria a inbox no Chatwoot via API dele (`autoCreate`) usando accountId+token
   informados pelo admin.
2. **Fluxo de saída**: webhook do Chatwoot (mensagem do agente) → handler novo →
   `/send/*` do Evolution GO.
3. **Fluxo de entrada**: no handler de evento WhatsApp (`*events.Message`), se a
   instância tem Chatwoot habilitado, POST pra API do Chatwoot criando/atualizando
   Contact+Conversation+Message. **Revisão cuidadosa aqui** — código de alto tráfego.
4. **Import de contatos/mensagens** (opcional, complexidade maior): ler histórico do
   whatsmeow no momento do QR code conectar, respeitando `daysLimitImportMessages`.
5. **UI**: painel por instância (sidebar "Integrations → Chatwoot", como no print),
   reusando o padrão de formulário já usado pro LDAP no Admin.

## Decisões em aberto

- [ ] Import de histórico é obrigatório na v1, ou fica pra depois?
- [ ] Config por instância (proposta A) ou só 1 Chatwoot pra todo o servidor?
- [ ] Vale a pena, nesse projeto, também resolver a gravação de mensagens reais no
  banco (bloco 4 acima) — beneficiaria também o Dashboard (contador de mensagens real).
