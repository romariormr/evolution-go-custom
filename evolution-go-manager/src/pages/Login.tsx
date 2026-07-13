import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, AlertDescription, AlertTitle, Button, Input, Label } from '@evoapi/design-system';
import { AlertCircle, KeyRound, UserRound } from 'lucide-react';
import { toast } from 'sonner';
import useAuth from '@/hooks/useAuth';
import { initRegister } from '@/services/api/license';
import useBrandingStore from '@/store/brandingStore';

type LoginMode = 'session' | 'legacy';

export default function Login() {
  const auth = useAuth();
  const navigate = useNavigate();
  const { branding } = useBrandingStore();
  const [mode, setMode] = useState<LoginMode>('session');
  const [apiUrl, setApiUrl] = useState(auth.apiUrl || window.location.origin);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [apiKey, setApiKey] = useState(auth.apiKey || '');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (auth.isAuthenticated && (auth.authMode === 'session' || auth.licenseState === 'licensed')) {
      navigate(auth.user?.mustChangePassword ? '/manager/change-password' : '/manager', { replace: true });
    }
  }, [auth.isAuthenticated, auth.authMode, auth.licenseState, auth.user, navigate]);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      if (mode === 'session') {
        const user = await auth.loginWithPassword(apiUrl, username, password);
        toast.success(`Bem-vindo${user.displayName ? `, ${user.displayName}` : ''}!`);
        navigate(user.mustChangePassword ? '/manager/change-password' : '/manager', { replace: true });
      } else {
        const cleanUrl = apiUrl.replace(/\/$/, '');
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
      const message = (caught as { message?: string })?.message || 'Não foi possível entrar.';
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

          {error && <Alert variant="destructive" className="mb-4"><AlertCircle className="h-4 w-4" /><AlertTitle>Erro ao entrar</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}

          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2"><Label htmlFor="apiUrl">URL da API</Label><Input id="apiUrl" value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} required disabled={loading} /></div>
            {mode === 'session' ? <>
              <div className="space-y-2"><Label htmlFor="username">Usuário</Label><Input id="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required disabled={loading} /></div>
              <div className="space-y-2"><Label htmlFor="password">Senha</Label><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required disabled={loading} /></div>
            </> : <div className="space-y-2"><Label htmlFor="apiKey">API Key (GLOBAL_API_KEY)</Label><Input id="apiKey" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} required minLength={10} disabled={loading} /><p className="text-xs text-muted-foreground">Fallback administrativo compatível com o fluxo anterior.</p></div>}
            <Button type="submit" className="w-full" disabled={loading}>{loading ? 'Entrando...' : 'Entrar'}</Button>
          </form>
        </div>
      </div>
    </div>
  );
}
