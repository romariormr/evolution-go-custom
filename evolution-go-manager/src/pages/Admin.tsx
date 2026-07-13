import { useCallback, useEffect, useState } from 'react';
import { Button, Input, Label } from '@evoapi/design-system';
import { Loader2, PlugZap, RefreshCw, Save, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import * as accessApi from '@/services/api/access';
import * as instancesApi from '@/services/api/instances';
import type { AccessGroup, AccessUser } from '@/types/auth';
import type { Instance } from '@/types/instance';
import useAuth from '@/hooks/useAuth';
import useBrandingStore from '@/store/brandingStore';

type Section = 'users' | 'groups' | 'links' | 'settings';

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
  { key: 'ldap.bind_password', label: 'Senha da conta de serviço', kind: 'password' },
  { key: 'ldap.base_dn', label: 'Base DN (busca)', placeholder: 'OU=GrupoNewland,DC=dominio,DC=local', kind: 'text' },
  { key: 'ldap.user_filter', label: 'Filtro de usuário', placeholder: '(sAMAccountName=%s)', kind: 'text' },
  { key: 'ldap.group_attribute', label: 'Atributo de grupo', placeholder: 'memberOf', kind: 'text' },
  { key: 'ldap.start_tls', label: 'StartTLS (ldap:// + upgrade)', kind: 'boolean' },
  { key: 'ldap.skip_verify_tls', label: 'Ignorar erro de certificado TLS', kind: 'boolean' },
];

export default function Admin() {
  const { user: currentUser } = useAuth();
  const [section, setSection] = useState<Section>('users');
  const [users, setUsers] = useState<AccessUser[]>([]);
  const [groups, setGroups] = useState<AccessGroup[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [newUser, setNewUser] = useState({ username: '', password: '', displayName: '', role: 'user', groupIds: [] as string[] });
  const [newGroup, setNewGroup] = useState({ name: '', ldapGroupDn: '' });
  const [link, setLink] = useState({ groupId: '', instanceId: '' });
  const [newSetting, setNewSetting] = useState({ key: '', value: '' });
  const [testingLdap, setTestingLdap] = useState(false);

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

  const createUser = async (event: React.FormEvent) => {
    event.preventDefault();
    try {
      await accessApi.createUser(newUser);
      setNewUser({ username: '', password: '', displayName: '', role: 'user', groupIds: [] });
      toast.success('Usuário criado.'); await load();
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const toggleUserGroup = async (user: AccessUser, groupId: string) => {
    const ids = user.groups?.map((group) => group.id) || [];
    const next = ids.includes(groupId) ? ids.filter((id) => id !== groupId) : [...ids, groupId];
    try { await accessApi.setUserGroups(user.id, next); toast.success('Grupos atualizados.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const resetPassword = async (user: AccessUser) => {
    const password = window.prompt(`Nova senha temporária para ${user.username} (mínimo 8 caracteres):`);
    if (!password) return;
    try { await accessApi.resetUserPassword(user.id, password); toast.success('Senha redefinida; a troca será exigida no próximo login.'); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const removeUser = async (user: AccessUser) => {
    if (!window.confirm(`Excluir o usuário ${user.username}?`)) return;
    try { await accessApi.deleteUser(user.id); toast.success('Usuário removido.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const createGroup = async (event: React.FormEvent) => {
    event.preventDefault();
    try { await accessApi.createGroup(newGroup); setNewGroup({ name: '', ldapGroupDn: '' }); toast.success('Grupo criado.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const removeGroup = async (group: AccessGroup) => {
    if (!window.confirm(`Excluir o grupo ${group.name}?`)) return;
    try { await accessApi.deleteGroup(group.id); toast.success('Grupo removido.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const changeLink = async (operation: 'link' | 'unlink') => {
    if (!link.groupId || !link.instanceId) return toast.error('Selecione um grupo e uma instância.');
    try {
      await (operation === 'link' ? accessApi.linkInstance : accessApi.unlinkInstance)(link.groupId, link.instanceId);
      toast.success(operation === 'link' ? 'Instância vinculada.' : 'Vínculo removido.');
    } catch (error) { toast.error(errorMessage(error)); }
  };

  const saveSetting = async (key: string, value: string) => {
    try { await accessApi.setSetting(key, value); setSettings((state) => ({ ...state, [key]: value })); toast.success('Configuração salva.'); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const saveBrandingConfig = async () => {
    try {
      await Promise.all(BRANDING_FIELDS.map((field) => accessApi.setSetting(field.key, settings[field.key] ?? '')));
      await useBrandingStore.getState().refresh();
      toast.success('Marca atualizada.');
    } catch (error) { toast.error(errorMessage(error)); }
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

  if (loading) return <div className="flex h-full items-center justify-center"><Loader2 className="h-7 w-7 animate-spin" /></div>;

  return <div className="mx-auto max-w-6xl space-y-6 p-6">
    <div className="flex items-center justify-between"><div><h1 className="text-2xl font-bold">Administração</h1><p className="text-sm text-muted-foreground">Usuários, grupos, vínculos e configurações do Manager.</p></div><Button variant="outline" onClick={() => void load()}><RefreshCw className="mr-2 h-4 w-4" />Atualizar</Button></div>
    <div className="flex flex-wrap gap-2 border-b pb-3">{(['users', 'groups', 'links', 'settings'] as Section[]).map((item) => <Button key={item} variant={section === item ? 'default' : 'outline'} onClick={() => setSection(item)}>{({ users: 'Usuários', groups: 'Grupos', links: 'Grupo ↔ Instância', settings: 'Settings' } as const)[item]}</Button>)}</div>

    {section === 'users' && <div className="space-y-6">
      <form onSubmit={createUser} className="grid gap-3 rounded-lg border p-4 md:grid-cols-5">
        <div><Label>Usuário</Label><Input value={newUser.username} onChange={(e) => setNewUser({ ...newUser, username: e.target.value })} required /></div>
        <div><Label>Nome</Label><Input value={newUser.displayName} onChange={(e) => setNewUser({ ...newUser, displayName: e.target.value })} /></div>
        <div><Label>Senha inicial</Label><Input type="password" minLength={8} value={newUser.password} onChange={(e) => setNewUser({ ...newUser, password: e.target.value })} required /></div>
        <div><Label>Perfil</Label><select className="h-10 w-full rounded-md border bg-background px-3" value={newUser.role} onChange={(e) => setNewUser({ ...newUser, role: e.target.value })}><option value="user">Usuário</option><option value="admin">Administrador</option></select></div>
        <div className="flex items-end"><Button className="w-full">Criar usuário</Button></div>
        {groups.length > 0 && <div className="md:col-span-5"><Label>Grupos iniciais</Label><div className="mt-2 flex flex-wrap gap-3">{groups.map((group) => <label key={group.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={newUser.groupIds.includes(group.id)} onChange={() => setNewUser({ ...newUser, groupIds: newUser.groupIds.includes(group.id) ? newUser.groupIds.filter((id) => id !== group.id) : [...newUser.groupIds, group.id] })} />{group.name}</label>)}</div></div>}
      </form>
      <div className="overflow-x-auto rounded-lg border"><table className="w-full text-sm"><thead className="bg-muted"><tr><th className="p-3 text-left">Usuário</th><th className="p-3 text-left">Perfil</th><th className="p-3 text-left">Grupos</th><th className="p-3 text-right">Ações</th></tr></thead><tbody>{users.map((user) => <tr key={user.id} className="border-t"><td className="p-3"><div className="font-medium">{user.displayName || user.username}</div><div className="text-muted-foreground">{user.username} · {user.authSource}{user.mustChangePassword ? ' · troca pendente' : ''}</div></td><td className="p-3">{user.role}</td><td className="p-3"><div className="flex flex-wrap gap-2">{groups.map((group) => <label key={group.id} className="flex items-center gap-1"><input type="checkbox" checked={user.groups?.some((item) => item.id === group.id) || false} onChange={() => void toggleUserGroup(user, group.id)} />{group.name}</label>)}</div></td><td className="p-3 text-right"><Button variant="outline" className="mr-2" onClick={() => void resetPassword(user)}>Redefinir senha</Button><Button variant="outline" disabled={user.id === currentUser?.id} onClick={() => void removeUser(user)}><Trash2 className="h-4 w-4" /></Button></td></tr>)}</tbody></table></div>
    </div>}

    {section === 'groups' && <div className="space-y-5"><form onSubmit={createGroup} className="grid gap-3 rounded-lg border p-4 md:grid-cols-[1fr_2fr_auto]"><div><Label>Nome</Label><Input value={newGroup.name} onChange={(e) => setNewGroup({ ...newGroup, name: e.target.value })} required /></div><div><Label>DN LDAP (opcional)</Label><Input value={newGroup.ldapGroupDn} onChange={(e) => setNewGroup({ ...newGroup, ldapGroupDn: e.target.value })} /></div><div className="flex items-end"><Button>Criar grupo</Button></div></form><div className="grid gap-3 md:grid-cols-2">{groups.map((group) => <div key={group.id} className="flex items-center justify-between rounded-lg border p-4"><div><div className="font-medium">{group.name}</div><div className="text-xs text-muted-foreground break-all">{group.ldapGroupDn || 'Sem vínculo LDAP'}</div></div><Button variant="outline" onClick={() => void removeGroup(group)}><Trash2 className="h-4 w-4" /></Button></div>)}</div></div>}

    {section === 'links' && <div className="max-w-2xl space-y-4 rounded-lg border p-5"><p className="text-sm text-muted-foreground">Selecione os dois itens e aplique ou remova o vínculo. A API atual não expõe uma consulta dos vínculos existentes.</p><div><Label>Grupo</Label><select className="h-10 w-full rounded-md border bg-background px-3" value={link.groupId} onChange={(e) => setLink({ ...link, groupId: e.target.value })}><option value="">Selecione...</option>{groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}</select></div><div><Label>Instância</Label><select className="h-10 w-full rounded-md border bg-background px-3" value={link.instanceId} onChange={(e) => setLink({ ...link, instanceId: e.target.value })}><option value="">Selecione...</option>{instances.map((instance) => <option key={instance.id} value={instance.id}>{instance.instanceName}</option>)}</select></div><div className="flex gap-2"><Button onClick={() => void changeLink('link')}>Vincular</Button><Button variant="outline" onClick={() => void changeLink('unlink')}>Remover vínculo</Button></div></div>}

    {section === 'settings' && <div className="space-y-6">
      <div className="rounded-lg border p-5">
        <div className="mb-4"><h3 className="font-semibold">Marca (Branding)</h3><p className="text-sm text-muted-foreground">Nome e logo exibidos no login e no menu — vazio usa o padrão "Evolution GO".</p></div>
        <div className="grid gap-4 md:grid-cols-2">
          {BRANDING_FIELDS.map((field) => (
            <div key={field.key} className="space-y-2">
              <Label>{field.label}</Label>
              <Input placeholder={field.placeholder} value={settings[field.key] ?? ''} onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })} />
            </div>
          ))}
        </div>
        <Button className="mt-4" onClick={() => void saveBrandingConfig()}><Save className="mr-2 h-4 w-4" />Salvar marca</Button>
      </div>

      <div className="rounded-lg border p-5">
        <div className="mb-4 flex items-center justify-between">
          <div><h3 className="font-semibold">Autenticação LDAP / Active Directory</h3><p className="text-sm text-muted-foreground">Login por conta do domínio e sincronização automática de grupos via memberOf.</p></div>
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
        <Button className="mt-4" onClick={() => void saveLdapConfig()}><Save className="mr-2 h-4 w-4" />Salvar configuração LDAP</Button>
      </div>

      <div className="space-y-4">
        <h3 className="font-semibold">Outras configurações</h3>
        <div className="rounded-lg border"><div className="grid grid-cols-[minmax(12rem,1fr)_2fr_auto] gap-3 border-b bg-muted p-3 font-medium"><span>Chave</span><span>Valor</span><span /></div>{Object.entries(settings).filter(([key]) => !key.startsWith('ldap.') && !key.startsWith('branding.')).map(([key, value]) => <div key={key} className="grid grid-cols-[minmax(12rem,1fr)_2fr_auto] gap-3 border-b p-3 last:border-0"><code className="self-center text-xs">{key}</code><Input value={value} onChange={(e) => setSettings({ ...settings, [key]: e.target.value })} /><Button variant="outline" onClick={() => void saveSetting(key, settings[key])}><Save className="h-4 w-4" /></Button></div>)}</div>
        <form onSubmit={(e) => { e.preventDefault(); if (newSetting.key) { void saveSetting(newSetting.key, newSetting.value); setNewSetting({ key: '', value: '' }); } }} className="grid gap-3 rounded-lg border p-4 md:grid-cols-[1fr_2fr_auto]"><Input placeholder="nova.chave" value={newSetting.key} onChange={(e) => setNewSetting({ ...newSetting, key: e.target.value })} required /><Input placeholder="Valor" value={newSetting.value} onChange={(e) => setNewSetting({ ...newSetting, value: e.target.value })} /><Button>Adicionar</Button></form>
      </div>
    </div>}
  </div>;
}
