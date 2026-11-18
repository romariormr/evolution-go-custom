import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Input, Label } from '@evoapi/design-system';
import { KeyRound, Loader2, LogOut, Monitor, ShieldCheck, ShieldOff } from 'lucide-react';
import { toast } from 'sonner';
import * as accessApi from '@/services/api/access';
import type { AccessSession } from '@/types/auth';
import useAuth from '@/hooks/useAuth';
import { describeAgent, formatDate } from '@/utils/sessionFormat';

const errorMessage = (error: unknown) => (error as { message?: string })?.message || 'A operação não pôde ser concluída.';

export default function Account() {
  const { user, authMode, refreshUser } = useAuth();
  const navigate = useNavigate();
  const [sessions, setSessions] = useState<AccessSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [setup, setSetup] = useState<{ secret: string; qrCode: string } | null>(null);
  const [code, setCode] = useState('');
  const [disableForm, setDisableForm] = useState({ password: '', code: '' });
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (authMode !== 'session') { setLoading(false); return; }
    setLoading(true);
    try { setSessions(await accessApi.mySessions()); await refreshUser(); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setLoading(false); }
  }, [authMode, refreshUser]);

  useEffect(() => { void load(); }, [load]);

  if (authMode !== 'session') {
    return <div className="p-6 text-sm text-muted-foreground">No modo legado (API key) não há conta de usuário.</div>;
  }

  const revoke = async (session: AccessSession) => {
    if (!window.confirm('Encerrar essa sessão? Quem estiver usando será desconectado.')) return;
    try { await accessApi.revokeMySession(session.id); toast.success('Sessão encerrada.'); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
  };

  const startSetup = async () => {
    setBusy(true);
    try { const result = await accessApi.startTotpSetup(); setSetup({ secret: result.secret, qrCode: result.qrCode }); setCode(''); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setBusy(false); }
  };

  const confirmSetup = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try { await accessApi.enableTotp(code); toast.success('Verificação em duas etapas ligada.'); setSetup(null); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setBusy(false); }
  };

  const disable = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try { await accessApi.disableTotp(disableForm.password, disableForm.code); toast.success('Verificação em duas etapas desligada.'); setDisableForm({ password: '', code: '' }); await load(); }
    catch (error) { toast.error(errorMessage(error)); }
    finally { setBusy(false); }
  };

  if (loading) return <div className="flex h-full items-center justify-center"><Loader2 className="h-7 w-7 animate-spin" /></div>;

  return <div className="mx-auto max-w-4xl space-y-6 p-6">
    <div>
      <h1 className="text-2xl font-bold">Minha conta</h1>
      <p className="text-sm text-muted-foreground">
        {user?.displayName || user?.username} · {user?.email || user?.username} · {user?.authSource === 'ldap' ? 'conta do AD' : 'conta local'}
      </p>
    </div>

    <div className="rounded-lg border p-5">
      <div className="flex items-center justify-between gap-4">
        <div><h3 className="font-semibold">Senha</h3><p className="text-sm text-muted-foreground">{user?.authSource === 'ldap' ? 'A senha é a mesma do Active Directory.' : 'Ao trocar a senha, as outras sessões abertas são encerradas.'}</p></div>
        {user?.authSource !== 'ldap' && <Button variant="outline" onClick={() => navigate('/manager/change-password')}><KeyRound className="mr-2 h-4 w-4" />Trocar senha</Button>}
      </div>
    </div>

    <div className="rounded-lg border p-5 space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h3 className="font-semibold">Verificação em duas etapas</h3>
          <p className="text-sm text-muted-foreground">Além da senha, pede o código de 6 números do app autenticador (Google Authenticator, Microsoft Authenticator, Authy).</p>
        </div>
        {user?.totpEnabled
          ? <span className="flex items-center gap-1 text-sm font-medium text-green-600"><ShieldCheck className="h-4 w-4" />Ligada</span>
          : !setup && <Button onClick={() => void startSetup()} disabled={busy}><ShieldCheck className="mr-2 h-4 w-4" />Ligar</Button>}
      </div>

      {setup && !user?.totpEnabled && <form onSubmit={confirmSetup} className="grid gap-4 md:grid-cols-[240px_1fr]">
        {setup.qrCode ? <img src={setup.qrCode} alt="QR code do app autenticador" className="h-60 w-60 rounded border bg-white p-2" /> : <div />}
        <div className="space-y-3">
          <p className="text-sm">1. No app autenticador, adicione uma conta lendo o QR code.</p>
          <p className="text-sm">Sem câmera? Digite a chave: <code className="break-all rounded bg-muted px-1 text-xs">{setup.secret}</code></p>
          <p className="text-sm">2. Digite o código que aparece no app:</p>
          <Input inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} required />
          <div className="flex gap-2"><Button disabled={busy || code.length !== 6}>Confirmar</Button><Button type="button" variant="outline" onClick={() => setSetup(null)}>Cancelar</Button></div>
        </div>
      </form>}

      {user?.totpEnabled && <form onSubmit={disable} className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
        {user.authSource !== 'ldap' && <div><Label>Senha atual</Label><Input type="password" value={disableForm.password} onChange={(e) => setDisableForm({ ...disableForm, password: e.target.value })} required /></div>}
        <div><Label>Código do app</Label><Input inputMode="numeric" maxLength={6} value={disableForm.code} onChange={(e) => setDisableForm({ ...disableForm, code: e.target.value.replace(/\D/g, '') })} required /></div>
        <div className="flex items-end"><Button variant="outline" disabled={busy}><ShieldOff className="mr-2 h-4 w-4" />Desligar</Button></div>
      </form>}
    </div>

    <div className="rounded-lg border">
      <div className="border-b p-4"><h3 className="font-semibold">Sessões abertas</h3><p className="text-sm text-muted-foreground">Onde sua conta está conectada agora. Encerre as que não reconhecer.</p></div>
      {sessions.length === 0 && <p className="p-4 text-sm text-muted-foreground">Nenhuma sessão.</p>}
      {sessions.map((session) => <div key={session.id} className="flex items-center justify-between gap-4 border-b p-4 last:border-0">
        <div className="flex items-center gap-3">
          <Monitor className="h-5 w-5 text-muted-foreground" />
          <div>
            <div className="font-medium">{describeAgent(session.userAgent)} {session.current && <span className="ml-2 rounded bg-primary/10 px-2 py-0.5 text-xs text-primary">esta sessão</span>}</div>
            <div className="text-xs text-muted-foreground">IP {session.ip || '—'} · entrou {formatDate(session.createdAt)} · último uso {formatDate(session.lastSeenAt)}</div>
          </div>
        </div>
        {!session.current && <Button variant="outline" onClick={() => void revoke(session)}><LogOut className="mr-2 h-4 w-4" />Encerrar</Button>}
      </div>)}
    </div>
  </div>;
}
