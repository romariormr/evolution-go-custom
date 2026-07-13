import { useState } from 'react';
import { Navigate, useNavigate } from 'react-router-dom';
import { Alert, AlertDescription, Button, Input, Label } from '@evoapi/design-system';
import { toast } from 'sonner';
import useAuth from '@/hooks/useAuth';

export default function ChangePassword() {
  const auth = useAuth();
  const navigate = useNavigate();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  if (!auth.isAuthenticated || auth.authMode !== 'session') return <Navigate to="/manager/login" replace />;

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (newPassword.length < 8) return setError('A nova senha deve ter pelo menos 8 caracteres.');
    if (newPassword !== confirmation) return setError('A confirmação não corresponde à nova senha.');
    setLoading(true); setError('');
    try {
      await auth.changePassword(currentPassword, newPassword);
      toast.success('Senha alterada com sucesso.');
      navigate('/manager', { replace: true });
    } catch (caught) { setError((caught as { message?: string })?.message || 'Não foi possível alterar a senha.'); }
    finally { setLoading(false); }
  };

  return <div className="min-h-screen flex items-center justify-center bg-background p-4"><div className="w-full max-w-md rounded-lg border p-6 shadow">
    <h1 className="text-2xl font-bold">Defina uma nova senha</h1><p className="mt-2 text-sm text-muted-foreground">A troca é obrigatória antes de acessar o Manager.</p>
    {error && <Alert variant="destructive" className="mt-4"><AlertDescription>{error}</AlertDescription></Alert>}
    <form onSubmit={submit} className="mt-5 space-y-4">
      <div className="space-y-2"><Label htmlFor="current">Senha atual</Label><Input id="current" type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} required /></div>
      <div className="space-y-2"><Label htmlFor="new">Nova senha</Label><Input id="new" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} minLength={8} required /></div>
      <div className="space-y-2"><Label htmlFor="confirmation">Confirmar nova senha</Label><Input id="confirmation" type="password" value={confirmation} onChange={(e) => setConfirmation(e.target.value)} minLength={8} required /></div>
      <Button className="w-full" disabled={loading}>{loading ? 'Salvando...' : 'Alterar senha'}</Button>
    </form>
  </div></div>;
}
