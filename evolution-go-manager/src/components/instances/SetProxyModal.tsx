/**
 * SetProxyModal Component
 * Modal for setting, updating or removing proxy configuration on an existing instance
 */

import { useState, useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Button,
  Input,
  Label,
} from '@evoapi/design-system';
import { Network, Loader2, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import * as instancesApi from '@/services/api/instances';
import type { Instance } from '@/types/instance';

const proxySchema = z.object({
  host: z.string().min(1, 'Host é obrigatório'),
  port: z.string().min(1, 'Porta é obrigatória').regex(/^\d+$/, 'Porta deve ser numérica'),
  username: z.string().optional(),
  password: z.string().optional(),
  protocol: z.string().optional(),
});

type ProxyFormData = z.infer<typeof proxySchema>;

interface SetProxyModalProps {
  instance: Instance | null;
  open: boolean;
  onClose: () => void;
  onSuccess?: () => void;
}

export default function SetProxyModal({ instance, open, onClose, onSuccess }: SetProxyModalProps) {
  const [isSaving, setIsSaving] = useState(false);
  const [isRemoving, setIsRemoving] = useState(false);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ProxyFormData>({
    resolver: zodResolver(proxySchema),
    defaultValues: {
      host: '',
      port: '',
      username: '',
      password: '',
      protocol: 'http',
    },
  });

  useEffect(() => {
    if (open) {
      reset({ host: '', port: '', username: '', password: '', protocol: 'http' });
    }
  }, [open, reset]);

  const handleSave = async (data: ProxyFormData) => {
    if (!instance) return;
    setIsSaving(true);
    try {
      await instancesApi.setInstanceProxy(instance.id, {
        host: data.host,
        port: data.port,
        username: data.username || undefined,
        password: data.password || undefined,
        protocol: data.protocol || 'http',
      });
      toast.success(`Proxy configurado para ${instance.instanceName}. Reconectando...`);
      onSuccess?.();
      onClose();
    } catch (error) {
      console.error('Erro ao configurar proxy:', error);
      toast.error(error instanceof Error ? error.message : 'Erro ao configurar proxy');
    } finally {
      setIsSaving(false);
    }
  };

  const handleRemove = async () => {
    if (!instance) return;
    setIsRemoving(true);
    try {
      await instancesApi.removeInstanceProxy(instance.id);
      toast.success(`Proxy removido de ${instance.instanceName}. Reconectando sem proxy...`);
      onSuccess?.();
      onClose();
    } catch (error) {
      console.error('Erro ao remover proxy:', error);
      toast.error(error instanceof Error ? error.message : 'Erro ao remover proxy');
    } finally {
      setIsRemoving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sidebar-foreground">
            <Network className="h-5 w-5 text-blue-400" />
            Configurar Proxy
          </DialogTitle>
          <DialogDescription className="text-sidebar-foreground/70">
            Instância: <strong>{instance?.instanceName}</strong>
            <br />
            Configure ou altere o proxy da instância. A instância será reconectada automaticamente.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(handleSave)} className="space-y-4">
          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2 space-y-1">
              <Label className="text-sidebar-foreground text-xs">Host *</Label>
              <Input
                placeholder="proxy.exemplo.com"
                {...register('host')}
                className="bg-sidebar border-sidebar-border text-sidebar-foreground placeholder:text-sidebar-foreground/40"
              />
              {errors.host && (
                <p className="text-xs text-red-400">{errors.host.message}</p>
              )}
            </div>
            <div className="space-y-1">
              <Label className="text-sidebar-foreground text-xs">Porta *</Label>
              <Input
                placeholder="8080"
                {...register('port')}
                className="bg-sidebar border-sidebar-border text-sidebar-foreground placeholder:text-sidebar-foreground/40"
              />
              {errors.port && (
                <p className="text-xs text-red-400">{errors.port.message}</p>
              )}
            </div>
          </div>

          <div className="space-y-1">
            <Label className="text-sidebar-foreground text-xs">Protocolo</Label>
            <select
              {...register('protocol')}
              className="w-full h-9 rounded-md border border-sidebar-border bg-sidebar px-3 text-sm text-sidebar-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            >
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
              <option value="socks5">SOCKS5</option>
              <option value="socks4">SOCKS4</option>
            </select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label className="text-sidebar-foreground text-xs">Usuário</Label>
              <Input
                placeholder="usuario"
                autoComplete="off"
                {...register('username')}
                className="bg-sidebar border-sidebar-border text-sidebar-foreground placeholder:text-sidebar-foreground/40"
              />
            </div>
            <div className="space-y-1">
              <Label className="text-sidebar-foreground text-xs">Senha</Label>
              <Input
                type="password"
                placeholder="••••••••"
                autoComplete="new-password"
                {...register('password')}
                className="bg-sidebar border-sidebar-border text-sidebar-foreground placeholder:text-sidebar-foreground/40"
              />
            </div>
          </div>

          <DialogFooter className="flex gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              onClick={handleRemove}
              disabled={isRemoving || isSaving}
              className="text-red-400 border-red-400/30 hover:bg-red-500/10 hover:text-red-300"
            >
              {isRemoving ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : (
                <Trash2 className="h-4 w-4 mr-2" />
              )}
              Remover Proxy
            </Button>

            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={isSaving || isRemoving}
              className="bg-sidebar border-sidebar-border text-sidebar-foreground hover:bg-sidebar-accent"
            >
              Cancelar
            </Button>

            <Button
              type="submit"
              disabled={isSaving || isRemoving}
              className="bg-blue-600 hover:bg-blue-700 text-white"
            >
              {isSaving ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : (
                <Network className="h-4 w-4 mr-2" />
              )}
              Salvar Proxy
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
