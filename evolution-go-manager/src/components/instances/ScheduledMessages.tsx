import { useCallback, useEffect, useState } from "react";
import { CalendarClock, Pause, Play, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import * as scheduleApi from "@/services/api/scheduledMessages";
import type { CreateSchedulePayload, ScheduledMessage } from "@/services/api/scheduledMessages";

interface Props {
  instanceId: string;
  instanceToken: string;
}

const statusStyle: Record<string, string> = {
  scheduled: "bg-blue-500/15 text-blue-500",
  sending: "bg-yellow-500/15 text-yellow-500",
  sent: "bg-green-500/15 text-green-500",
  failed: "bg-red-500/15 text-red-500",
  paused: "bg-muted text-muted-foreground",
};

const statusLabel: Record<string, string> = {
  scheduled: "agendada",
  sending: "enviando",
  sent: "enviada",
  failed: "falhou",
  paused: "pausada",
};

const inputClass =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring";

const fmt = (iso?: string) => (iso ? new Date(iso).toLocaleString("pt-BR") : "—");

const summary = (s: ScheduledMessage) => {
  try {
    const p = JSON.parse(s.payload);
    return p.text || p.caption || p.question || p.url || s.type;
  } catch {
    return s.type;
  }
};

/**
 * Mensagens agendadas da instância: cria envios únicos (data/hora) ou
 * recorrentes (cron), lista, pausa/retoma e apaga. O envio passa pela fila
 * anti-ban da instância.
 */
export function ScheduledMessages({ instanceId, instanceToken }: Props) {
  const [items, setItems] = useState<ScheduledMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);

  const [type, setType] = useState<"text" | "media">("text");
  const [number, setNumber] = useState("");
  const [text, setText] = useState("");
  const [mediaUrl, setMediaUrl] = useState("");
  const [mediaType, setMediaType] = useState("image");
  const [mode, setMode] = useState<"once" | "cron">("once");
  const [sendAt, setSendAt] = useState("");
  const [cron, setCron] = useState("0 9 * * 1-5");
  const [description, setDescription] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await scheduleApi.listSchedules(instanceId, instanceToken));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar agendamentos");
    } finally {
      setLoading(false);
    }
  }, [instanceId, instanceToken]);

  useEffect(() => {
    load();
  }, [load]);

  const create = async () => {
    if (!number.trim()) return toast.error("Informe o número ou JID");
    if (type === "text" && !text.trim()) return toast.error("Informe o texto");
    if (type === "media" && !mediaUrl.trim()) return toast.error("Informe a URL da mídia");
    if (mode === "once" && !sendAt) return toast.error("Informe data e hora");

    const payload: CreateSchedulePayload = {
      type,
      description: description.trim() || undefined,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "America/Sao_Paulo",
      payload:
        type === "text"
          ? { number: number.trim(), text }
          : { number: number.trim(), url: mediaUrl.trim(), type: mediaType, caption: text },
    };
    if (mode === "once") payload.sendAt = new Date(sendAt).toISOString();
    else payload.cron = cron.trim();

    setSaving(true);
    try {
      const s = await scheduleApi.createSchedule(instanceId, instanceToken, payload);
      toast.success(`Agendado para ${fmt(s.nextRunAt)}`);
      setText("");
      setDescription("");
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao agendar");
    } finally {
      setSaving(false);
    }
  };

  const act = async (id: string, fn: () => Promise<unknown>, ok: string) => {
    setBusyId(id);
    try {
      await fn();
      toast.success(ok);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro");
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Mensagens agendadas</h2>
          <p className="text-xs text-muted-foreground">
            Envio único (data/hora) ou recorrente (cron). Os envios passam pela fila anti-ban da instância.
          </p>
        </div>
        <Button type="button" variant="outline" onClick={load} disabled={loading} className="gap-2">
          <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Atualizar
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-2 mb-6">
        <div>
          <label className="text-sm font-medium text-foreground">Número ou JID</label>
          <input className={inputClass} value={number} onChange={(e) => setNumber(e.target.value)} placeholder="5511999999999" />
        </div>
        <div>
          <label className="text-sm font-medium text-foreground">Tipo</label>
          <select className={inputClass} value={type} onChange={(e) => setType(e.target.value as "text" | "media")}>
            <option value="text">Texto</option>
            <option value="media">Mídia (URL)</option>
          </select>
        </div>
        {type === "media" && (
          <>
            <div>
              <label className="text-sm font-medium text-foreground">URL da mídia</label>
              <input className={inputClass} value={mediaUrl} onChange={(e) => setMediaUrl(e.target.value)} placeholder="https://..." />
            </div>
            <div>
              <label className="text-sm font-medium text-foreground">Tipo de mídia</label>
              <select className={inputClass} value={mediaType} onChange={(e) => setMediaType(e.target.value)}>
                <option value="image">Imagem</option>
                <option value="video">Vídeo</option>
                <option value="audio">Áudio</option>
                <option value="document">Documento</option>
              </select>
            </div>
          </>
        )}
        <div className="md:col-span-2">
          <label className="text-sm font-medium text-foreground">{type === "text" ? "Texto" : "Legenda"}</label>
          <textarea className={inputClass} rows={3} value={text} onChange={(e) => setText(e.target.value)} />
        </div>
        <div>
          <label className="text-sm font-medium text-foreground">Quando</label>
          <select className={inputClass} value={mode} onChange={(e) => setMode(e.target.value as "once" | "cron")}>
            <option value="once">Uma vez (data e hora)</option>
            <option value="cron">Recorrente (cron)</option>
          </select>
        </div>
        <div>
          {mode === "once" ? (
            <>
              <label className="text-sm font-medium text-foreground">Data e hora</label>
              <input type="datetime-local" className={inputClass} value={sendAt} onChange={(e) => setSendAt(e.target.value)} />
            </>
          ) : (
            <>
              <label className="text-sm font-medium text-foreground">Cron (min hora dia mês dia-semana)</label>
              <input className={inputClass} value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 9 * * 1-5" />
              <p className="text-xs text-muted-foreground mt-1">Ex.: "0 9 * * 1-5" = dias úteis às 09:00.</p>
            </>
          )}
        </div>
        <div className="md:col-span-2">
          <label className="text-sm font-medium text-foreground">Descrição (opcional)</label>
          <input className={inputClass} value={description} onChange={(e) => setDescription(e.target.value)} />
        </div>
        <div className="md:col-span-2 flex justify-end">
          <Button type="button" onClick={create} disabled={saving} className="gap-2">
            <CalendarClock className="h-4 w-4" />
            {saving ? "Agendando..." : "Agendar"}
          </Button>
        </div>
      </div>

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground py-6 text-center">
          {loading ? "Carregando..." : "Nenhuma mensagem agendada."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted-foreground border-b border-border">
                <th className="py-2 pr-3">Próximo envio</th>
                <th className="py-2 pr-3">Para</th>
                <th className="py-2 pr-3">Mensagem</th>
                <th className="py-2 pr-3">Recorrência</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2"></th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id} className="border-b border-border/50 align-top">
                  <td className="py-2 pr-3 whitespace-nowrap text-muted-foreground">
                    {fmt(s.nextRunAt)}
                    {s.lastRunAt && <div className="text-xs">último: {fmt(s.lastRunAt)}</div>}
                  </td>
                  <td className="py-2 pr-3">{s.number}</td>
                  <td className="py-2 pr-3 max-w-xs">
                    <div className="truncate text-foreground" title={summary(s)}>
                      {s.description || summary(s)}
                    </div>
                    {s.lastError && <div className="text-xs text-red-500 break-words">{s.lastError}</div>}
                  </td>
                  <td className="py-2 pr-3 whitespace-nowrap">
                    {s.cronExpr ? <code className="text-xs">{s.cronExpr}</code> : "única"}
                    {s.runCount > 0 && <div className="text-xs text-muted-foreground">{s.runCount}x</div>}
                  </td>
                  <td className="py-2 pr-3">
                    <span className={`rounded px-2 py-0.5 text-xs ${statusStyle[s.status] || ""}`}>
                      {statusLabel[s.status] || s.status}
                    </span>
                  </td>
                  <td className="py-2 whitespace-nowrap">
                    <div className="flex gap-1">
                      {s.status === "scheduled" && (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-8 px-2"
                          title="Pausar"
                          disabled={busyId === s.id}
                          onClick={() => act(s.id, () => scheduleApi.setSchedulePaused(instanceId, instanceToken, s.id, true), "Pausado")}
                        >
                          <Pause className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {s.status === "paused" && (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-8 px-2"
                          title="Retomar"
                          disabled={busyId === s.id}
                          onClick={() => act(s.id, () => scheduleApi.setSchedulePaused(instanceId, instanceToken, s.id, false), "Retomado")}
                        >
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {s.status !== "sending" && (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-8 px-2"
                          title="Apagar"
                          disabled={busyId === s.id}
                          onClick={() => {
                            if (confirm("Apagar este agendamento?"))
                              act(s.id, () => scheduleApi.deleteSchedule(instanceId, instanceToken, s.id), "Apagado");
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
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
