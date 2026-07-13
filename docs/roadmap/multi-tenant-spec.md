# Spec — Multi-tenant / Perfis de Acesso

> Status: ✅ **CONCLUÍDO** (Fases 1, 2 e 3) — em produção desde `v0.9.0`.
> Branches: `feature/multi-tenant` (Fase 1+3), `feature/ldap-auth` (Fase 2), ambas mergeadas em `main`.
> Detalhes de acesso e checklist pós-projeto: `HANDOFF-SESSAO.md` (local, fora do git).

## Objetivo

Hoje: API key global (vê tudo) + token por instância. Sem usuários.
Meta: usuários e **grupos** (setor/empresa); cada grupo enxerga só as próprias instâncias.
Tudo administrável **pela página do Manager** — incluindo ativação opcional de LDAP/AD.

## Modelo de dados (banco `evogo_users`)

```sql
CREATE TABLE evogo_admin_users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username TEXT UNIQUE NOT NULL,
  password_hash TEXT,              -- NULL quando origem = ldap
  display_name TEXT,
  role TEXT NOT NULL DEFAULT 'user',  -- 'admin' | 'user'
  auth_source TEXT NOT NULL DEFAULT 'local', -- 'local' | 'ldap'
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE evogo_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT UNIQUE NOT NULL,          -- ex.: "TI", "Marketing", "Empresa X"
  ldap_group_dn TEXT,                 -- opcional: DN do grupo de segurança do AD
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE evogo_user_groups (
  user_id UUID REFERENCES evogo_admin_users(id) ON DELETE CASCADE,
  group_id UUID REFERENCES evogo_groups(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, group_id)
);

CREATE TABLE evogo_group_instances (
  group_id UUID REFERENCES evogo_groups(id) ON DELETE CASCADE,
  instance_id UUID NOT NULL,          -- FK lógica p/ instância (evogo_auth)
  PRIMARY KEY (group_id, instance_id)
);

CREATE TABLE evogo_settings (          -- config LDAP e afins, editável na UI
  key TEXT PRIMARY KEY,               -- 'ldap.enabled', 'ldap.url', 'ldap.bind_dn', ...
  value TEXT NOT NULL
);
```

## Backend (Go/Gin)

- `POST /auth/login` → user/senha. Fluxo: se `ldap.enabled` e user não-local → bind LDAP; senão bcrypt local. Retorna JWT de sessão (cookie httpOnly).
- Middleware de sessão nas rotas do Manager API.
- `GET /instance/all`: admin → todas; user → só instâncias dos grupos dele.
- Criação de instância por user → vincula automaticamente ao(s) grupo(s) dele.
- CRUD admin: `/admin/users`, `/admin/groups`, `/admin/groups/:id/instances`, `/admin/settings/ldap` (+ `POST /admin/settings/ldap/test` pra validar bind antes de salvar).
- API key global continua funcionando (automação/n8n do admin). Tokens de instância inalterados.
- Bootstrap: primeiro boot sem usuários → cria admin com senha = GLOBAL_API_KEY (forçar troca no primeiro login).

## LDAP (opcional, ativável na UI)

Config na tela de admin (persistida em `evogo_settings`):
- URL (`ldaps://ad.dominio.local:636`), Bind DN + senha (conta de serviço), Base DN de busca,
  filtro de usuário (`(sAMAccountName=%s)`), atributo de grupo (`memberOf`).
- Mapeamento: grupo de segurança AD (DN) → grupo evo-go (campo `ldap_group_dn`).
- Login LDAP: bind da conta de serviço → busca DN do usuário → re-bind com a senha do usuário → lê `memberOf` → sincroniza grupos.
- Botão **Testar conexão** na UI antes de ativar.
- Lib: `github.com/go-ldap/ldap/v3`.

## Manager (React)

- Tela de login (user/senha) substituindo login por API key (API key vira opção "login admin por chave").
- Menu **Administração** (só admin): Usuários, Grupos, Vínculo grupo↔instâncias, Configurações → LDAP.
- Usuário comum: vê/gerencia só instâncias dos seus grupos; sem acesso à key global.

## n8n

Sem mudança: cada setor usa o **token da própria instância** na credencial do node
[@romariormr/n8n-nodes-evogo](https://www.npmjs.com/package/@romariormr/n8n-nodes-evogo)
(opção "— Usar Chave Da Credencial —"). Key global fica só com admins.

## Fases

1. **Dados + auth local + filtro por grupo** (backend) — entrega isolamento já funcional via API
2. **LDAP/AD** com config pela UI + testar conexão
3. **Telas admin no Manager** (usuários, grupos, LDAP, vínculos)

## Decisões em aberto

- [ ] Sessão: JWT em cookie httpOnly (proposto) vs header
- [ ] User comum pode deletar instância do grupo? (proposto: sim, com confirm)
- [ ] Auditoria de ações admin (log em tabela) — fase 2+
