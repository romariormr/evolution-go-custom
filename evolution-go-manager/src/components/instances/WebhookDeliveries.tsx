import { useCallback, useEffect, useState } from "react";
import { RefreshCw, RotateCcw, Send } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import * as deliveriesApi from "@/services/api/webhookDeliveries";
import type { WebhookDelivery } from "@/services/api/webhookDeliveries";

interface Props {
  instanceId: string;
  instanceToken: string;
}

const statusStyle: Record<string, string> = {
  success: "bg-green-500/15 text-green-500",
  failed: "bg-red-500/15 text-red-500",
  pending: "bg-yellow-500/15 text-yellow-500",
};

const statusLabel: Record<string, string> = {
  success: "entregue",
  failed: "falhou",
  pending: "tentando",
};

/**
 * Log de entregas de webhook da instância: lista as tentativas (por padrão o
 * servidor só registra as que falharam), permite reenviar uma entrega e
 * disparar um teste para os webhooks configurados.
 */
export function WebhookDeliveries({ instanceId, instanceToken }: Props) {
  const [items, setItems] = useState<WebhookDelivery[]>([]);
  const [filter, setFilter] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await deliveriesApi.listWebhookDeliveries(instanceId, instanceToken, filter || undefined));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar entregas");
    } finally {
      setLoading(false);
    }
  }, [instanceId, instanceToken, filter]);

  useEffect(() => {
    load();
  }, [load]);

  const resend = async (id: string) => {
    setBusyId(id);
    try {
      const d = await deliveriesApi.resendWebhookDelivery(instanceId, instanceToken, id);
      if (d.status === "success") toast.success("Reenviado com sucesso");
      else toast.error(`Reenvio falhou: ${d.lastError || d.httpStatus}`);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao reenviar");
    } finally {
      setBusyId(null);
    }
  };

  const runTest = async () => {
    setTesting(true);
    try {
      const results = await deliveriesApi.testWebhook(instanceId, instanceToken);
      results.forEach((r) =>
        r.ok
          ? toast.success(`${r.url}: OK (HTTP ${r.httpStatus}, ${r.latencyMs} ms)`)
          : toast.error(`${r.url}: falhou - ${r.error || `HTTP ${r.httpStatus}`}`)
      );
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao testar webhook");
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Entregas de webhook</h2>
          <p className="text-xs text-muted-foreground">
            Tentativas de entrega dos eventos (por padrão só as que falharam ficam registradas).
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="rounded-md border border-input bg-background px-2 py-1.5 text-sm text-foreground"
          >
            <option value="">Todas</option>
            <option value="failed">Falhas</option>
            <option value="pending">Tentando</option>
            <option value="success">Entregues</option>
          </select>
          <Button type="button" variant="outline" onClick={load} disabled={loading} className="gap-2">
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Atualizar
          </Button>
          <Button type="button" onClick={runTest} disabled={testing} className="gap-2">
            <Send className="h-4 w-4" />
            {testing ? "Testando..." : "Testar webhook"}
          </Button>
        </div>
      </div>

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground py-6 text-center">
          {loading ? "Carregando..." : "Nenhuma entrega registrada."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted-foreground border-b border-border">
                <th className="py-2 pr-3">Quando</th>
                <th className="py-2 pr-3">Evento</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2 pr-3">HTTP</th>
                <th className="py-2 pr-3">Tent.</th>
                <th className="py-2 pr-3">Destino / erro</th>
                <th className="py-2"></th>
              </tr>
            </thead>
            <tbody>
              {items.map((d) => (
                <tr key={d.id} className="border-b border-border/50 align-top">
                  <td className="py-2 pr-3 whitespace-nowrap text-muted-foreground">
                    {new Date(d.updatedAt || d.createdAt).toLocaleString("pt-BR")}
                  </td>
                  <td className="py-2 pr-3">{d.event}</td>
                  <td className="py-2 pr-3">
                    <span className={`rounded px-2 py-0.5 text-xs ${statusStyle[d.status] || ""}`}>
                      {statusLabel[d.status] || d.status}
                    </span>
                  </td>
                  <td className="py-2 pr-3">{d.httpStatus || "—"}</td>
                  <td className="py-2 pr-3">{d.attempts}</td>
                  <td className="py-2 pr-3 max-w-md">
                    <div className="truncate text-foreground" title={d.url}>
                      {d.url}
                    </div>
                    {d.lastError && (
                      <div className="text-xs text-red-500 break-words" title={d.response}>
                        {d.lastError}
                      </div>
                    )}
                  </td>
                  <td className="py-2">
                    {d.status !== "success" && (
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => resend(d.id)}
                        disabled={busyId === d.id}
                        className="gap-1 h-8 px-2"
                      >
                        <RotateCcw className="h-3.5 w-3.5" />
                        Reenviar
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
