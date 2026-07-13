import { useEffect, useMemo, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { Loader2, Smartphone, Wifi, WifiOff } from 'lucide-react';
import useInstancesStore from '@/store/instancesStore';
import EmptyState from '@/components/base/EmptyState';

function StatTile({ icon: Icon, label, value, tone }: { icon: typeof Smartphone; label: string; value: number; tone: 'default' | 'success' | 'muted' }) {
  const toneClass = tone === 'success' ? 'text-emerald-500' : tone === 'muted' ? 'text-muted-foreground' : 'text-primary';
  return (
    <div className="rounded-lg border p-4 flex items-center gap-3">
      <div className={`rounded-md bg-muted p-2 ${toneClass}`}>
        <Icon className="h-5 w-5" />
      </div>
      <div>
        <div className="text-2xl font-bold">{value}</div>
        <div className="text-sm text-muted-foreground">{label}</div>
      </div>
    </div>
  );
}

export default function Dashboard() {
  const navigate = useNavigate();
  const { instances, isLoading, hasLoaded, fetchInstances } = useInstancesStore();
  const initialFetchDone = useRef(false);

  useEffect(() => {
    if (!initialFetchDone.current) {
      fetchInstances();
      initialFetchDone.current = true;
    }
  }, [fetchInstances]);

  const { total, connected, disconnected } = useMemo(() => {
    const connectedCount = instances.filter((i) => i.connected).length;
    return { total: instances.length, connected: connectedCount, disconnected: instances.length - connectedCount };
  }, [instances]);

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold text-foreground">Dashboard</h1>

      {!hasLoaded && isLoading ? (
        <div className="flex h-32 items-center justify-center"><Loader2 className="h-6 w-6 animate-spin" /></div>
      ) : (
        <>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <StatTile icon={Smartphone} label="Instâncias" value={total} tone="default" />
            <StatTile icon={Wifi} label="Conectadas" value={connected} tone="success" />
            <StatTile icon={WifiOff} label="Desconectadas" value={disconnected} tone="muted" />
          </div>

          <div>
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-lg font-semibold">Suas instâncias</h2>
              <button onClick={() => navigate('/manager/instances')} className="text-sm text-primary hover:underline">
                Ver todas
              </button>
            </div>

            {total === 0 ? (
              <EmptyState
                icon={Smartphone}
                title="Nenhuma instância ainda"
                description="Crie sua primeira instância na aba Instâncias."
                action={{ label: 'Ir para Instâncias', onClick: () => navigate('/manager/instances') }}
              />
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {instances.map((instance) => (
                  <button
                    key={instance.id}
                    onClick={() => navigate(`/manager/instances/${instance.id}/settings`)}
                    className="rounded-lg border p-4 text-left hover:bg-accent transition-colors"
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-medium truncate">{instance.instanceName}</span>
                      <span
                        className={`text-xs rounded-full px-2 py-0.5 ${
                          instance.connected
                            ? 'bg-emerald-500/10 text-emerald-500'
                            : 'bg-muted text-muted-foreground'
                        }`}
                      >
                        {instance.connected ? 'Conectado' : 'Desconectado'}
                      </span>
                    </div>
                    {instance.owner && (
                      <div className="mt-1 text-sm text-muted-foreground">{instance.owner}</div>
                    )}
                  </button>
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
