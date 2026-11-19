import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, AlertDescription, AlertTitle, Button, Input, Label } from '@evoapi/design-system';
import { AlertCircle, Info, KeyRound, UserRound } from 'lucide-react';
import { toast } from 'sonner';
import useAuth from '@/hooks/useAuth';
import { initRegister } from '@/services/api/license';
import useBrandingStore from '@/store/brandingStore';

type LoginMode = 'session' | 'legacy';

// Endereço completo (http/https). Evita o "Erro desconhecido" quando o
// gerenciador de senhas do navegador preenche o usuário no campo da URL.
const isValidUrl = (url: string) => /^https?:\/\/[^\s/]+/i.test(url.trim());

export default function Login() {
  const auth = useAuth();
  const navigate = useNavigate();
  const { branding, refresh: refreshBranding } = useBrandingStore();
  const [mode, setMode] = useState<LoginMode>('session');
  const [apiUrl, setApiUrl] = useState(auth.apiUrl && isValidUrl(auth.apiUrl) ? auth.apiUrl : window.location.origin);
  const [editUrl, setEditUrl] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [apiKey, setApiKey] = useState(auth.apiKey || '');
  const [error, setError] = useState('');
  const [challenge, setChallenge] = useState('');
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(false);

  // Recarrega a cada abertura do login: firstAccess muda depois da 1ª troca de senha.
  useEffect(() => { refreshBranding(); }, [refreshBranding]);
  useEffect(() => {
    if (branding.firstAccess) setUsername((current) => current || 'admin');
  }, [branding.firstAccess]);

  useEffect(() => {
    if (auth.isAuthenticated && (auth.authMode === 'session' || auth.licenseState === 'licensed')) {
      navigate(auth.user?.mustChangePassword ? '/manager/change-password' : '/manager', { replace: true });
    }
  }, [auth.isAuthenticated, auth.authMode, auth.licenseState, auth.user, navigate]);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const cleanUrl = apiUrl.trim().replace(/\/+$/, '');
    if (!isValidUrl(cleanUrl)) {
      setEditUrl(true);
      setError(`URL da API inválida: "${apiUrl}". Use o endereço completo, por exemplo ${window.location.origin}`);
      return;
    }
    setLoading(true);
    setError('');
    try {
      if (mode === 'session') {
        let user;
        if (challenge) {
          user = await auth.completeTwoFactor(cleanUrl, challenge, code);
        } else {
          const outcome = await auth.loginWithPassword(cleanUrl, username, password);
          if (outcome.twoFactorRequired) {
            setChallenge(outcome.challenge);
            setCode('');
            return;
          }
          user = outcome.user;
        }
        toast.success(`Bem-vindo${user.displayName ? `, ${user.displayName}` : ''}!`);
        navigate(user.mustChangePassword ? '/manager/change-password' : '/manager', { replace: true });
      } else {
        const license = await auth.checkLicense(cleanUrl, apiKey);
        if (license !== 'licensed') {
          const callbackUrl = `${window.location.origin}/manager/license/callback`;
          const registration = await initRegister(callbackUrl, cleanUrl, apiKey);
          if (!registration.register_url) throw new Error(registration.message || 'Falha ao iniciar o registro da licença.');
          auth.setApiUrl(cleanUrl);
          auth.setApiKey(apiKey);
          window.location.href = registration.register_url;
          return;
        }
        await auth.login(cleanUrl, apiKey);
        toast.success('Conectado no modo legado.');
        navigate('/manager', { replace: true });
      }
    } catch (caught) {
      const message = (caught as { response?: { data?: { error?: string } } })?.response?.data?.error
        || (caught as { message?: string })?.message || 'Não foi possível entrar.';
      if (message.includes('tempo para digitar')) setChallenge('');
      setError(message);
      toast.error(message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center p-4 bg-gradient-to-t from-primary/20 via-background/95 to-background">
      <div className="w-full max-w-md space-y-6">
        <div className="flex flex-col items-center gap-2">
          {branding.logo && (
            <img src={branding.logo} alt={branding.appName} className="h-12 w-12 rounded object-contain" />
          )}
          <h1 className="text-center text-3xl font-bold text-primary">{branding.appName}</h1>
        </div>
        <div className="rounded-lg border bg-background/80 p-6 shadow-lg backdrop-blur-sm">
          <h2 className="text-2xl font-bold">Acessar o Manager</h2>
          <p className="mt-1 text-sm text-muted-foreground">Entre com seu usuário ou use a API key administrativa.</p>

          <div className="my-5 grid grid-cols-2 rounded-md bg-muted p-1">
            <button type="button" onClick={() => setMode('session')} className={`flex items-center justify-center gap-2 rounded px-3 py-2 text-sm ${mode === 'session' ? 'bg-background shadow' : ''}`}>
              <UserRound className="h-4 w-4" /> Usuário e senha
            </button>
            <button type="button" onClick={() => setMode('legacy')} className={`flex items-center justify-center gap-2 rounded px-3 py-2 text-sm ${mode === 'legacy' ? 'bg-background shadow' : ''}`}>
              <KeyRound className="h-4 w-4" /> Modo legado
            </button>
          </div>

          {branding.firstAccess && mode === 'session' && !challenge && (
            <div className="mb-4 rounded-md border border-primary/40 bg-primary/10 p-3 text-sm">
              <p className="flex items-center gap-2 font-semibold"><Info className="h-4 w-4" /> Primeiro acesso</p>
              <ul className="mt-2 list-disc space-y-1 pl-5 text-muted-foreground">
                <li>Usuário: <code className="text-foreground">admin</code> (já preenchido)</li>
                <li>Senha: o valor da variável <code className="text-foreground">GLOBAL_API_KEY</code> do deploy (Portainer → Stacks → evolution-go → Environment variables, ou o arquivo <code className="text-foreground">.env</code>)</li>
                <li>Logo depois você define uma senha nova, só sua.</li>
              </ul>
            </div>
          )}

          {error && <Alert variant="destructive" className="mb-4"><AlertCircle className="h-4 w-4" /><AlertTitle>Erro ao entrar</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}

          <form onSubmit={submit} className="space-y-4">
            {editUrl || mode === 'legacy' ? (
              <div className="space-y-2">
                <Label htmlFor="apiUrl">URL da API</Label>
                <Input id="apiUrl" name="evogo-api-url" type="url" inputMode="url" autoComplete="off" placeholder={window.location.origin} value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} required disabled={loading} />
                <p className="text-xs text-muted-foreground">Normalmente é o próprio endereço desta página: {window.location.origin}</p>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                Servidor: <span className="text-foreground">{apiUrl}</span>{' '}
                <button type="button" className="underline" onClick={() => setEditUrl(true)}>alterar</button>
              </p>
            )}
            {mode === 'session' ? (challenge ? <>
              <div className="space-y-2">
                <Label htmlFor="code">Código do app autenticador</Label>
                <Input id="code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} autoFocus value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} required disabled={loading} />
                <p className="text-xs text-muted-foreground">Abra o Google/Microsoft Authenticator e digite os 6 números.</p>
              </div>
              <button type="button" className="text-xs text-muted-foreground underline" onClick={() => { setChallenge(''); setCode(''); }}>Voltar</button>
            </> : <>
              <div className="space-y-2"><Label htmlFor="username">Usuário ou e-mail</Label><Input id="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required disabled={loading} /></div>
              <div className="space-y-2"><Label htmlFor="password">Senha</Label><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required disabled={loading} /></div>
            </>) : <div className="space-y-2"><Label htmlFor="apiKey">API Key (GLOBAL_API_KEY)</Label><Input id="apiKey" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} required minLength={10} disabled={loading} /><p className="text-xs text-muted-foreground">Acesso de emergência com a chave mestra (GLOBAL_API_KEY). Não identifica quem entrou: prefira usuário e senha.</p></div>}
            <Button type="submit" className="w-full" disabled={loading}>{loading ? 'Entrando...' : challenge ? 'Confirmar código' : 'Entrar'}</Button>
          </form>
        </div>
      </div>
    </div>
  );
}
