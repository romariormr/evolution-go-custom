import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Input, Label } from '@evoapi/design-system';
import { Copy, KeyRound, Loader2, LogOut, Monitor, PlugZap, RefreshCw, Save, ShieldOff, Trash2, Upload, UserCheck, Users } from 'lucide-react';
import { toast } from 'sonner';
import * as accessApi from '@/services/api/access';
import * as instancesApi from '@/services/api/instances';
import type { AccessAudit, AccessGroup, AccessSession, AccessUser } from '@/types/auth';
import type { Instance } from '@/types/instance';
import useAuth from '@/hooks/useAuth';
import useBrandingStore from '@/store/brandingStore';
import { describeAgent, formatDate } from '@/utils/sessionFormat';

type Section = 'users' | 'groups' | 'links' | 'audit' | 'settings';

const SECTION_LABELS: Record<Section, string> = {
  users: 'Usuários',
  groups: 'Grupos',
  links: 'Compartilhar instâncias',
  audit: 'Auditoria',
  settings: 'Configurações',
};

const errorMessage = (error: unknown) => (error as { message?: string })?.message || 'A operação não pôde ser concluída.';

type LdapField = { key: string; label: string; placeholder?: string; kind: 'text' | 'password' | 'boolean' };

const BRANDING_FIELDS: LdapField[] = [
  { key: 'branding.app_name', label: 'Nome do sistema', placeholder: 'Evolution GO', kind: 'text' },
  { key: 'branding.logo', label: 'Logo (URL ou data URI)', placeholder: 'https://.../logo.png', kind: 'text' },
];

const LDAP_FIELDS: LdapField[] = [
  { key: 'ldap.enabled', label: 'Habilitado', kind: 'boolean' },
  { key: 'ldap.url', label: 'URL', placeholder: 'ldaps://ad.dominio.local:636', kind: 'text' },
  { key: 'ldap.bind_dn', label: 'Bind DN (conta de serviço)', placeholder: 'CN=svc-evogo,OU=Integracoes,DC=dominio,DC=local', kind: 'text' },
  { key: 'ldap.bind_password', label: 'Senha da conta de serviço (fica cifrada)', kind: 'password' },
  { key: 'ldap.base_dn', label: 'Base DN (busca)', placeholder: 'OU=Empresa,DC=dominio,DC=local', kind: 'text' },
  { key: 'ldap.user_filter', label: 'Filtro de usuário', placeholder: '(sAMAccountName=%s)', kind: 'text' },
  { key: 'ldap.group_attribute', label: 'Atributo de grupo', placeholder: 'memberOf', kind: 'text' },
  { key: 'ldap.admin_group_dn', label: 'Grupo do AD que vira administrador (opcional)', placeholder: 'CN=GG-EvoGo-Admins,OU=Grupos,DC=dominio,DC=local', kind: 'text' },
  { key: 'ldap.start_tls', label: 'StartTLS (ldap:// + upgrade)', kind: 'boolean' },
  { key: 'ldap.skip_verify_tls', label: 'Ignorar erro de certificado TLS', kind: 'boolean' },
];

const AUDIT_LABELS: Record<string, string> = {
  login: 'Entrou', login_failed: 'Falha de login', logout: 'Saiu', session_revoked: 'Sessão encerrada',
  password_changed: 'Trocou a senha', password_reset: 'Senha redefinida', '2fa_enabled': 'Ligou 2FA',
  '2fa_disabled': 'Desligou 2FA', '2fa_reset': '2FA removido pelo admin', user_created: 'Usuário criado',
  user_updated: 'Usuário alterado', user_deleted: 'Usuário excluído', user_groups_changed: 'Grupos do usuário',
  group_created: 'Grupo criado', group_deleted: 'Grupo excluído', group_key_regenerated: 'Nova chave do grupo',
  instance_shared: 'Instância compartilhada', instance_unshared: 'Compartilhamento removido',
  instance_created: 'Instância criada', instance_deleted: 'Instância excluída', setting_changed: 'Configuração alterada',
};

const emptyUser = { username: '', email: '', password: '', displayName: '', role: 'user', seeAllInstances: true, groupIds: [] as string[] };

export default function Admin() {
  const { user: currentUser, refreshUser } = useAuth();
  const [section, setSection] = useState<Section>('users');
  const [users, setUsers] = useState<AccessUser[]>([]);
  const [groups, setGroups] = useState<AccessGroup[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [newUser, setNewUser] = useState(emptyUser);
  const [newGroup, setNewGroup] = useState({ name: '', ldapGroupDn: '' });
  const [sessionsOf, setSessionsOf] = useState<{ user: AccessUser; list: AccessSession[] } | null>(null);
  const [shareInstance, setShareInstance] = useState('');
  const [sharedWith, setSharedWith] = useState<string[]>([]);
  const [audit, setAudit] = useState<{ list: AccessAudit[]; total: number }>({ list: [], total: 0 });
  const [auditFilter, setAuditFilter] = useState({ username: '', action: '', offset: 0 });
  const [ldapTest, setLdapTest] = useState({ username: '', password: '' });
  const [ldapTestResult, setLdapTestResult] = useState<Awaited<ReturnType<typeof accessApi.testLdapUser>> | null>(null);
  const [testingLdap, setTestingLdap] = useState(false);
  const [uploadingLogo, setUploadingLogo] = useState(false);
  const logoInputRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [nextUsers, nextGroups, nextInstances, nextSettings] = await Promise.all([
        accessApi.listUsers(), accessApi.listGroups(), instancesApi.fetchInstances(), accessApi.listSettings(),
      ]);
      setUsers(nextUsers); setGroups(nextGroups); setInstances(nextInstances); setSettings(nextSettings);
    } catch (error) { toast.error(errorMessage(error)); }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const loadAudit = useCallback(async () => {
    try {
      const result = await accessApi.listAudit({ username: auditFilter.username || undefined, action: auditFilter.action || undefined, limit: 50, offset: auditFilter.offset });
      setAudit({ list: result.data, total: result.total });
    } catch (error) { toast.error(errorMessage(error)); }
  }, [auditFilter]);

  useEffect(() => { if (section === 'audit') void loadAudit(); }, [section, loadAudit]);

  useEffect(() => {
    if (!shareInstance) { setSharedWith([]); return; }
    accessApi.instanceGroups(shareInstance).then(setSharedWith).catch((error) => toast.error(errorMessage(error)));
  }, [shareInstance]);

  // ── usuários ──
  const createUser = async (event: React.FormEvent) => {
    event.preventDefault();
    try {
      await accessApi.createUser(newUser);
      setNewUser(emptyUser);
      toast.success('Perfil criado. No primeiro acesso a pessoa troca a senha.'); await load();
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const updateUser = async (user: AccessUser, payload: Parameters<typeof accessApi.updateUser>[1], message: string) => {
    try {
      await accessApi.updateUser(user.id, payload);
      toast.success(message);
      if (user.id === currentUser?.id) await refreshUser();
      await load();
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const toggleUserGroup = async (user: AccessUser, groupId: string) => {
    const ids = user.groups?.map((group) => group.id) || [];
    const next = ids.includes(groupId) ? ids.filter((id) => id !== groupId) : [...ids, groupId];
    try { await accessApi.setUserGroups(user.id, next); toast.success('Grupos atualizados.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const resetPassword = async (user: AccessUser) => {
    const password = window.prompt(`Nova senha temporária para ${user.username} (mínimo 8 caracteres, com letras e números):`);
    if (!password) return;
    try { await accessApi.resetUserPassword(user.id, password); toast.success('Senha redefinida; sessões encerradas e troca exigida no próximo login.'); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const resetTotp = async (user: AccessUser) => {
    if (!window.confirm(`Remover a verificação em duas etapas de ${user.username}? Use quando a pessoa perdeu o celular.`)) return;
    try { await accessApi.resetUserTotp(user.id); toast.success('2FA removido; a pessoa pode configurar de novo.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const showSessions = async (user: AccessUser) => {
    try { setSessionsOf({ user, list: await accessApi.listUserSessions(user.id) }); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const revokeSession = async (session: AccessSession) => {
    if (!sessionsOf || !window.confirm('Encerrar essa sessão?')) return;
    try { await accessApi.revokeSession(session.id); toast.success('Sessão encerrada.'); await showSessions(sessionsOf.user); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const removeUser = async (user: AccessUser) => {
    if (!window.confirm(`Excluir o usuário ${user.username}?`)) return;
    try { await accessApi.deleteUser(user.id); toast.success('Usuário removido.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  // ── grupos ──
  const createGroup = async (event: React.FormEvent) => {
    event.preventDefault();
    try { await accessApi.createGroup(newGroup); setNewGroup({ name: '', ldapGroupDn: '' }); toast.success('Grupo criado.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const removeGroup = async (group: AccessGroup) => {
    if (!window.confirm(`Excluir o grupo ${group.name}? Quem só via instâncias por ele perde o acesso.`)) return;
    try { await accessApi.deleteGroup(group.id); toast.success('Grupo removido.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const regenerateGroupKey = async (group: AccessGroup) => {
    if (!window.confirm(`Gerar nova chave de API para ${group.name}? A chave atual deixa de funcionar.`)) return;
    try { await accessApi.regenerateGroupApiKey(group.id); toast.success('Nova chave gerada.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const copyGroupKey = async (apiKey: string) => {
    try { await navigator.clipboard.writeText(apiKey); toast.success('Chave copiada.'); }
    catch { toast.error('Não foi possível copiar a chave.'); }
  };

  // ── compartilhar ──
  const toggleShare = async (groupId: string) => {
    if (!shareInstance) return;
    const shared = sharedWith.includes(groupId);
    try {
      await (shared ? accessApi.unlinkInstance : accessApi.linkInstance)(groupId, shareInstance);
      setSharedWith(shared ? sharedWith.filter((id) => id !== groupId) : [...sharedWith, groupId]);
      toast.success(shared ? 'Compartilhamento removido.' : 'Instância compartilhada com o grupo.');
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const membersOf = (groupId: string) => users.filter((user) => user.groups?.some((group) => group.id === groupId));

  // ── configurações ──
  const saveBrandingConfig = async () => {
    try {
      await Promise.all(BRANDING_FIELDS.map((field) => accessApi.setSetting(field.key, settings[field.key] ?? '')));
      await useBrandingStore.getState().refresh();
      toast.success('Marca atualizada.');
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const uploadLogo = async (file: File) => {
    setUploadingLogo(true);
    try {
      const url = await accessApi.uploadLogo(file);
      setSettings((state) => ({ ...state, 'branding.logo': url }));
      await useBrandingStore.getState().refresh();
      toast.success('Logo enviada pro MinIO.');
    } catch (error) { toast.error(errorMessage(error)); }
    finally { setUploadingLogo(false); }
  };

  const saveLdapConfig = async () => {
    try {
      await Promise.all(LDAP_FIELDS.map((field) => accessApi.setSetting(field.key, settings[field.key] ?? '')));
      toast.success('Configuração LDAP salva.');
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const testLdap = async () => {
    setTestingLdap(true);
    try { await accessApi.testLdap(); toast.success('Conexão LDAP bem-sucedida.'); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setTestingLdap(false); }
  };

  const testLdapUser = async (event: React.FormEvent) => {
    event.preventDefault();
    setTestingLdap(true); setLdapTestResult(null);
    try { setLdapTestResult(await accessApi.testLdapUser(ldapTest.username, ldapTest.password)); setLdapTest({ ...ldapTest, password: '' }); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setTestingLdap(false); }
  };

  if (loading) return <div className="flex h-full items-center justify-center"><Loader2 className="h-7 w-7 animate-spin" /></div>;

  const selectCls = 'h-10 w-full rounded-md border bg-background px-3';

  return <div className="mx-auto max-w-6xl space-y-6 p-6">
    <div className="flex items-center justify-between"><div><h1 className="text-2xl font-bold">Administração</h1><p className="text-sm text-muted-foreground">Perfis de acesso, grupos, compartilhamento de instâncias e auditoria.</p></div><Button variant="outline" onClick={() => void load()}><RefreshCw className="mr-2 h-4 w-4" />Atualizar</Button></div>
    <div className="flex flex-wrap gap-2 border-b pb-3">{(Object.keys(SECTION_LABELS) as Section[]).map((item) => <Button key={item} variant={section === item ? 'default' : 'outline'} onClick={() => setSection(item)}>{SECTION_LABELS[item]}</Button>)}</div>

    {section === 'users' && <div className="space-y-6">
      <form onSubmit={createUser} className="grid gap-3 rounded-lg border p-4 md:grid-cols-3">
        <div className="md:col-span-3"><h3 className="font-semibold">Novo perfil</h3><p className="text-sm text-muted-foreground">Quem não é administrador vê só as instâncias dos grupos dele.</p></div>
        <div><Label>E-mail</Label><Input type="email" value={newUser.email} onChange={(e) => setNewUser({ ...newUser, email: e.target.value })} placeholder="nome@empresa.com" /></div>
        <div><Label>Usuário (opcional — vazio usa o e-mail)</Label><Input value={newUser.username} onChange={(e) => setNewUser({ ...newUser, username: e.target.value })} /></div>
        <div><Label>Nome</Label><Input value={newUser.displayName} onChange={(e) => setNewUser({ ...newUser, displayName: e.target.value })} /></div>
        <div><Label>Senha inicial</Label><Input type="password" minLength={8} value={newUser.password} onChange={(e) => setNewUser({ ...newUser, password: e.target.value })} required /><p className="mt-1 text-xs text-muted-foreground">8+ caracteres, letras e números. Troca obrigatória no 1º acesso.</p></div>
        <div><Label>Perfil</Label><select className={selectCls} value={newUser.role} onChange={(e) => setNewUser({ ...newUser, role: e.target.value })}><option value="user">Usuário</option><option value="admin">Administrador</option></select></div>
        <div className="flex items-end">{newUser.role === 'admin' && <label className="flex items-center gap-2 pb-2 text-sm"><input type="checkbox" checked={newUser.seeAllInstances} onChange={(e) => setNewUser({ ...newUser, seeAllInstances: e.target.checked })} />Ver todas as instâncias</label>}</div>
        {groups.length > 0 && <div className="md:col-span-3"><Label>Grupos</Label><div className="mt-2 flex flex-wrap gap-3">{groups.map((group) => <label key={group.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={newUser.groupIds.includes(group.id)} onChange={() => setNewUser({ ...newUser, groupIds: newUser.groupIds.includes(group.id) ? newUser.groupIds.filter((id) => id !== group.id) : [...newUser.groupIds, group.id] })} />{group.name}</label>)}</div></div>}
        <div className="md:col-span-3"><Button disabled={!newUser.email && !newUser.username}>Criar perfil</Button></div>
      </form>

      <div className="overflow-x-auto rounded-lg border"><table className="w-full text-sm"><thead className="bg-muted"><tr><th className="p-3 text-left">Perfil</th><th className="p-3 text-left">Acesso</th><th className="p-3 text-left">Grupos</th><th className="p-3 text-right">Ações</th></tr></thead><tbody>
        {users.map((user) => {
          const self = user.id === currentUser?.id;
          const isAdmin = user.role === 'admin';
          return <tr key={user.id} className={`border-t align-top ${user.active === false ? 'opacity-60' : ''}`}>
            <td className="p-3">
              <div className="font-medium">{user.displayName || user.username}{self && <span className="ml-2 text-xs text-muted-foreground">(você)</span>}</div>
              <div className="text-muted-foreground">{user.email || user.username}</div>
              <div className="text-xs text-muted-foreground">{user.authSource === 'ldap' ? 'AD' : 'local'}{user.totpEnabled ? ' · 2FA' : ''}{user.mustChangePassword ? ' · troca de senha pendente' : ''}{user.active === false ? ' · DESATIVADO' : ''} · último acesso {formatDate(user.lastLoginAt)}</div>
            </td>
            <td className="p-3 space-y-2">
              <select className="h-9 rounded-md border bg-background px-2" value={user.role} disabled={self} onChange={(e) => void updateUser(user, { role: e.target.value }, 'Perfil alterado.')}><option value="user">Usuário</option><option value="admin">Administrador</option></select>
              {isAdmin && <label className="flex items-center gap-2"><input type="checkbox" checked={user.seeAllInstances !== false} onChange={(e) => void updateUser(user, { seeAllInstances: e.target.checked }, e.target.checked ? 'Passa a ver todas as instâncias.' : 'Passa a ver só as instâncias dos grupos dele.')} />Vê todas as instâncias</label>}
              <label className="flex items-center gap-2"><input type="checkbox" checked={user.active !== false} disabled={self} onChange={(e) => void updateUser(user, { active: e.target.checked }, e.target.checked ? 'Usuário reativado.' : 'Usuário desativado e desconectado.')} />Ativo</label>
            </td>
            <td className="p-3"><div className="flex flex-wrap gap-2">{groups.map((group) => <label key={group.id} className="flex items-center gap-1"><input type="checkbox" checked={user.groups?.some((item) => item.id === group.id) || false} onChange={() => void toggleUserGroup(user, group.id)} />{group.name}</label>)}{groups.length === 0 && <span className="text-xs text-muted-foreground">Crie um grupo primeiro</span>}</div></td>
            <td className="p-3 text-right"><div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" title="Sessões abertas" onClick={() => void showSessions(user)}><Monitor className="h-4 w-4" /></Button>
              {user.authSource !== 'ldap' && <Button variant="outline" title="Redefinir senha" onClick={() => void resetPassword(user)}><KeyRound className="h-4 w-4" /></Button>}
              {user.totpEnabled && <Button variant="outline" title="Remover 2FA" onClick={() => void resetTotp(user)}><ShieldOff className="h-4 w-4" /></Button>}
              <Button variant="outline" title="Excluir" disabled={self} onClick={() => void removeUser(user)}><Trash2 className="h-4 w-4" /></Button>
            </div></td>
          </tr>;
        })}
      </tbody></table></div>

      {sessionsOf && <div className="rounded-lg border">
        <div className="flex items-center justify-between border-b p-4"><h3 className="font-semibold">Sessões abertas de {sessionsOf.user.displayName || sessionsOf.user.username}</h3><Button variant="outline" onClick={() => setSessionsOf(null)}>Fechar</Button></div>
        {sessionsOf.list.length === 0 && <p className="p-4 text-sm text-muted-foreground">Nenhuma sessão aberta.</p>}
        {sessionsOf.list.map((session) => <div key={session.id} className="flex items-center justify-between gap-4 border-b p-4 last:border-0">
          <div><div className="font-medium">{describeAgent(session.userAgent)}{session.current && <span className="ml-2 text-xs text-primary">(esta sessão)</span>}</div><div className="text-xs text-muted-foreground">IP {session.ip || '—'} · entrou {formatDate(session.createdAt)} · último uso {formatDate(session.lastSeenAt)}</div></div>
          {!session.current && <Button variant="outline" onClick={() => void revokeSession(session)}><LogOut className="mr-2 h-4 w-4" />Encerrar</Button>}
        </div>)}
      </div>}
    </div>}

    {section === 'groups' && <div className="space-y-5">
      <p className="text-sm text-muted-foreground">Grupo = quem pode ver e usar quais instâncias. Coloque as pessoas no grupo (aba Usuários) e compartilhe as instâncias com ele (aba Compartilhar instâncias).</p>
      <form onSubmit={createGroup} className="grid gap-3 rounded-lg border p-4 md:grid-cols-[1fr_2fr_auto]"><div><Label>Nome</Label><Input value={newGroup.name} onChange={(e) => setNewGroup({ ...newGroup, name: e.target.value })} required /></div><div><Label>Grupo do AD (opcional)</Label><Input value={newGroup.ldapGroupDn} placeholder="CN=GG-Vendas,OU=Grupos,DC=dominio,DC=local" onChange={(e) => setNewGroup({ ...newGroup, ldapGroupDn: e.target.value })} /></div><div className="flex items-end"><Button>Criar grupo</Button></div></form>
      <div className="grid gap-3 md:grid-cols-2">{groups.map((group) => <div key={group.id} className="space-y-3 rounded-lg border p-4">
        <div className="flex items-center justify-between"><div><div className="font-medium">{group.name}</div><div className="text-xs text-muted-foreground break-all">{group.ldapGroupDn || 'Sem vínculo com o AD'}</div></div><Button variant="outline" onClick={() => void removeGroup(group)}><Trash2 className="h-4 w-4" /></Button></div>
        <div className="flex items-start gap-2 text-sm"><Users className="mt-0.5 h-4 w-4 text-muted-foreground" /><span>{membersOf(group.id).map((user) => user.displayName || user.username).join(', ') || <span className="text-muted-foreground">Sem membros</span>}</span></div>
        <div className="flex items-center justify-between gap-2 rounded-md bg-muted p-2"><div className="min-w-0"><div className="text-xs text-muted-foreground">Chave de API do grupo (n8n/integrações)</div><div className="truncate font-mono text-xs">{group.apiKey || 'gerando...'}</div></div><div className="flex shrink-0 gap-1"><Button variant="outline" title="Copiar chave" onClick={() => void copyGroupKey(group.apiKey)}><Copy className="h-4 w-4" /></Button><Button variant="outline" title="Gerar nova chave" onClick={() => void regenerateGroupKey(group)}><KeyRound className="h-4 w-4" /></Button></div></div>
      </div>)}</div>
    </div>}

    {section === 'links' && <div className="max-w-3xl space-y-4 rounded-lg border p-5">
      <div><h3 className="font-semibold">Compartilhar uma instância</h3><p className="text-sm text-muted-foreground">Escolha a instância e marque os grupos que podem usá-la. Todos os membros do grupo passam a ver a instância; os demais perfis não veem.</p></div>
      <div><Label>Instância</Label><select className={selectCls} value={shareInstance} onChange={(e) => setShareInstance(e.target.value)}><option value="">Selecione...</option>{instances.map((instance) => <option key={instance.id} value={instance.id}>{instance.instanceName}{instance.connected ? ' — conectada' : ' — desconectada'}</option>)}</select></div>
      {shareInstance && <div className="space-y-2">
        {groups.length === 0 && <p className="text-sm text-muted-foreground">Nenhum grupo criado ainda.</p>}
        {groups.map((group) => <label key={group.id} className="flex items-start gap-3 rounded-md border p-3">
          <input type="checkbox" className="mt-1" checked={sharedWith.includes(group.id)} onChange={() => void toggleShare(group.id)} />
          <div><div className="font-medium">{group.name}</div><div className="text-xs text-muted-foreground">{membersOf(group.id).map((user) => user.displayName || user.username).join(', ') || 'Sem membros'}</div></div>
        </label>)}
      </div>}
    </div>}

    {section === 'audit' && <div className="space-y-4">
      <div className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
        <Input placeholder="Filtrar por usuário" value={auditFilter.username} onChange={(e) => setAuditFilter({ ...auditFilter, username: e.target.value, offset: 0 })} />
        <select className={selectCls} value={auditFilter.action} onChange={(e) => setAuditFilter({ ...auditFilter, action: e.target.value, offset: 0 })}><option value="">Todas as ações</option>{Object.entries(AUDIT_LABELS).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select>
        <Button variant="outline" onClick={() => void loadAudit()}><RefreshCw className="mr-2 h-4 w-4" />Atualizar</Button>
      </div>
      <div className="overflow-x-auto rounded-lg border"><table className="w-full text-sm"><thead className="bg-muted"><tr><th className="p-3 text-left">Quando</th><th className="p-3 text-left">Quem</th><th className="p-3 text-left">Ação</th><th className="p-3 text-left">Alvo / detalhe</th><th className="p-3 text-left">IP</th></tr></thead><tbody>
        {audit.list.map((entry) => <tr key={entry.id} className={`border-t ${entry.success ? '' : 'text-red-600'}`}><td className="p-3 whitespace-nowrap">{formatDate(entry.createdAt)}</td><td className="p-3">{entry.username || '—'}</td><td className="p-3">{AUDIT_LABELS[entry.action] || entry.action}</td><td className="p-3 break-all">{[entry.target, entry.detail].filter(Boolean).join(' · ')}</td><td className="p-3">{entry.ip}</td></tr>)}
        {audit.list.length === 0 && <tr><td colSpan={5} className="p-4 text-center text-muted-foreground">Nada registrado.</td></tr>}
      </tbody></table></div>
      <div className="flex items-center justify-between text-sm text-muted-foreground"><span>{audit.total} registros</span><div className="flex gap-2"><Button variant="outline" disabled={auditFilter.offset === 0} onClick={() => setAuditFilter({ ...auditFilter, offset: Math.max(0, auditFilter.offset - 50) })}>Anterior</Button><Button variant="outline" disabled={auditFilter.offset + 50 >= audit.total} onClick={() => setAuditFilter({ ...auditFilter, offset: auditFilter.offset + 50 })}>Próxima</Button></div></div>
    </div>}

    {section === 'settings' && <div className="space-y-6">
      <div className="rounded-lg border p-5">
        <div className="mb-4"><h3 className="font-semibold">Marca</h3><p className="text-sm text-muted-foreground">Nome e logo exibidos no login e no menu — vazio usa o padrão "Evolution GO".</p></div>
        <div className="grid gap-4 md:grid-cols-2">
          {BRANDING_FIELDS.map((field) => (
            <div key={field.key} className="space-y-2">
              <Label>{field.label}</Label>
              {field.key === 'branding.logo' ? (
                <div className="flex gap-2">
                  <Input placeholder={field.placeholder} value={settings[field.key] ?? ''} onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })} />
                  <input ref={logoInputRef} type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml" className="hidden" onChange={(e) => { const file = e.target.files?.[0]; if (file) void uploadLogo(file); e.target.value = ''; }} />
                  <Button type="button" variant="outline" onClick={() => logoInputRef.current?.click()} disabled={uploadingLogo}>
                    {uploadingLogo ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
                  </Button>
                </div>
              ) : (
                <Input placeholder={field.placeholder} value={settings[field.key] ?? ''} onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })} />
              )}
            </div>
          ))}
        </div>
        <p className="mt-2 text-xs text-muted-foreground">Envie um arquivo (vai pro MinIO) ou cole uma URL/data URI direto no campo.</p>
        <Button className="mt-4" onClick={() => void saveBrandingConfig()}><Save className="mr-2 h-4 w-4" />Salvar marca</Button>
      </div>

      <div className="rounded-lg border p-5">
        <div className="mb-4 flex items-center justify-between">
          <div><h3 className="font-semibold">Login pelo Active Directory (opcional)</h3><p className="text-sm text-muted-foreground">Desligado, só os perfis locais entram. Ligado, contas do domínio entram e os grupos são sincronizados pelo memberOf.</p></div>
          <Button variant="outline" onClick={() => void testLdap()} disabled={testingLdap}>{testingLdap ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <PlugZap className="mr-2 h-4 w-4" />}Testar conexão</Button>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          {LDAP_FIELDS.map((field) => (
            <div key={field.key} className={field.kind === 'boolean' ? 'flex items-center gap-2 pt-6' : 'space-y-2'}>
              {field.kind === 'boolean' ? (
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={settings[field.key] === 'true'} onChange={(e) => setSettings({ ...settings, [field.key]: e.target.checked ? 'true' : 'false' })} />{field.label}</label>
              ) : (
                <><Label>{field.label}</Label><Input type={field.kind === 'password' ? 'password' : 'text'} placeholder={field.placeholder} value={settings[field.key] ?? ''} onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })} /></>
              )}
            </div>
          ))}
        </div>
        <Button className="mt-4" onClick={() => void saveLdapConfig()}><Save className="mr-2 h-4 w-4" />Salvar configuração do AD</Button>

        <form onSubmit={testLdapUser} className="mt-6 grid gap-3 border-t pt-4 md:grid-cols-[1fr_1fr_auto]">
          <div className="md:col-span-3"><h4 className="font-medium">Testar login de um usuário</h4><p className="text-xs text-muted-foreground">Confere senha e grupos no AD sem criar nada, e mostra o que a pessoa receberia aqui.</p></div>
          <Input placeholder="usuário do domínio" value={ldapTest.username} onChange={(e) => setLdapTest({ ...ldapTest, username: e.target.value })} required />
          <Input type="password" placeholder="senha" value={ldapTest.password} onChange={(e) => setLdapTest({ ...ldapTest, password: e.target.value })} required />
          <Button variant="outline" disabled={testingLdap}><UserCheck className="mr-2 h-4 w-4" />Testar</Button>
          {ldapTestResult && <div className="md:col-span-3 rounded-md bg-muted p-3 text-sm space-y-1">
            <div><b>{ldapTestResult.displayName}</b> {ldapTestResult.email && `· ${ldapTestResult.email}`}</div>
            <div>Papel aqui: <b>{ldapTestResult.wouldBeAdmin ? 'Administrador' : 'Usuário'}</b></div>
            <div>Grupos que receberia: {ldapTestResult.matchedGroups.join(', ') || 'nenhum (não verá instâncias)'}</div>
            <details><summary className="cursor-pointer text-xs text-muted-foreground">memberOf no AD ({ldapTestResult.memberOf.length})</summary><ul className="mt-1 text-xs">{ldapTestResult.memberOf.map((dn) => <li key={dn} className="break-all">{dn}</li>)}</ul></details>
          </div>}
        </form>
      </div>
    </div>}
  </div>;
}
