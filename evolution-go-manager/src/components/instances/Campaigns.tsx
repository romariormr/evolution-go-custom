import { useCallback, useEffect, useState } from "react";
import { Megaphone, Pause, Play, Plus, RefreshCw, RotateCcw, Square, Trash2 } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import * as campaignApi from "@/services/api/campaigns";
import type { Campaign, CampaignRecipient, OptOut } from "@/services/api/campaigns";

interface Props {
  instanceId: string;
  instanceToken: string;
}

const inputClass =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring";

const statusStyle: Record<string, string> = {
  draft: "bg-muted text-muted-foreground",
  running: "bg-blue-500/15 text-blue-500",
  paused: "bg-yellow-500/15 text-yellow-500",
  completed: "bg-green-500/15 text-green-500",
  canceled: "bg-red-500/15 text-red-500",
  pending: "bg-muted text-muted-foreground",
  sending: "bg-blue-500/15 text-blue-500",
  sent: "bg-green-500/15 text-green-500",
  failed: "bg-red-500/15 text-red-500",
  skipped: "bg-yellow-500/15 text-yellow-500",
};

const statusLabel: Record<string, string> = {
  draft: "rascunho",
  running: "enviando",
  paused: "pausada",
  completed: "concluída",
  canceled: "cancelada",
  pending: "na fila",
  sending: "enviando",
  sent: "enviada",
  failed: "falhou",
  skipped: "pulada",
};

/** Linhas "número" ou "número;nome" (também aceita vírgula ou tab como separador do nome). */
const parseRecipients = (raw: string) =>
  raw
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const [number, ...rest] = l.split(/[;\t]|,(?=\s*\D)/);
      return { number: number.trim(), name: rest.join(" ").trim() || undefined };
    });

/**
 * Campanhas: disparo para uma lista de números com progresso por destinatário,
 * pausar/retomar/cancelar e reenviar falhas. Os envios saem um por vez, com
 * intervalo aleatório e pela fila anti-ban; quem responde SAIR/PARAR é
 * descadastrado e pulado.
 */
export function Campaigns({ instanceId, instanceToken }: Props) {
  const [items, setItems] = useState<Campaign[]>([]);
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [recipients, setRecipients] = useState<CampaignRecipient[]>([]);
  const [recipientFilter, setRecipientFilter] = useState("");
  const [optOuts, setOptOuts] = useState<OptOut[] | null>(null);

  const [name, setName] = useState("");
  const [type, setType] = useState<"text" | "media">("text");
  const [text, setText] = useState("");
  const [mediaUrl, setMediaUrl] = useState("");
  const [mediaType, setMediaType] = useState("image");
  const [numbers, setNumbers] = useState("");
  const [delayMin, setDelayMin] = useState(3);
  const [delayMax, setDelayMax] = useState(8);
  const [startNow, setStartNow] = useState(true);
  const [consent, setConsent] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await campaignApi.listCampaigns(instanceId, instanceToken));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar campanhas");
    } finally {
      setLoading(false);
    }
  }, [instanceId, instanceToken]);

  useEffect(() => {
    load();
  }, [load]);

  // enquanto houver campanha rodando, atualiza o progresso a cada 10 s
  useEffect(() => {
    if (!items.some((c) => c.status === "running")) return;
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, [items, load]);

  const loadRecipients = useCallback(
    async (id: string, status: string) => {
      try {
        setRecipients(await campaignApi.listCampaignRecipients(instanceId, instanceToken, id, status || undefined));
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Erro ao carregar destinatários");
      }
    },
    [instanceId, instanceToken]
  );

  useEffect(() => {
    if (expanded) loadRecipients(expanded, recipientFilter);
  }, [expanded, recipientFilter, loadRecipients]);

  const parsed = parseRecipients(numbers);

  const create = async () => {
    if (type === "text" && !text.trim()) return toast.error("Informe a mensagem");
    if (type === "media" && !mediaUrl.trim()) return toast.error("Informe a URL da mídia");
    if (parsed.length === 0) return toast.error("Informe ao menos um número");
    if (!consent) return toast.error("Confirme que os contatos autorizaram receber mensagens");
    setSaving(true);
    try {
      const c = await campaignApi.createCampaign(instanceId, instanceToken, {
        name: name.trim(),
        type,
        message: type === "text" ? { text } : { mediaUrl: mediaUrl.trim(), mediaType, caption: text },
        recipients: parsed,
        delayMinMs: Math.max(0, delayMin) * 1000,
        delayMaxMs: Math.max(0, delayMax) * 1000,
        start: startNow,
      });
      toast.success(`Campanha criada com ${c.total} destinatários`);
      setShowForm(false);
      setName("");
      setText("");
      setNumbers("");
      setConsent(false);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao criar campanha");
    } finally {
      setSaving(false);
    }
  };

  const act = async (c: Campaign, action: "start" | "pause" | "cancel" | "retry") => {
    if (action === "cancel" && !confirm(`Cancelar a campanha "${c.name}"? Os pendentes não serão enviados.`)) return;
    setBusyId(c.id);
    try {
      await campaignApi.campaignAction(instanceId, instanceToken, c.id, action);
      await load();
      if (expanded === c.id) await loadRecipients(c.id, recipientFilter);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro");
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (c: Campaign) => {
    if (!confirm(`Apagar a campanha "${c.name}" e o histórico de envios?`)) return;
    setBusyId(c.id);
    try {
      await campaignApi.deleteCampaign(instanceId, instanceToken, c.id);
      if (expanded === c.id) setExpanded(null);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao apagar");
    } finally {
      setBusyId(null);
    }
  };

  const toggleOptOuts = async () => {
    if (optOuts) return setOptOuts(null);
    try {
      setOptOuts(await campaignApi.listOptOuts(instanceId, instanceToken));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar descadastros");
    }
  };

  const iconBtn = "h-8 px-2";

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Campanhas</h2>
          <p className="text-xs text-muted-foreground">
            Envio um a um, com intervalo aleatório e pela fila anti-ban. Quem responder SAIR ou PARAR é descadastrado.
            Dispare só para contatos que autorizaram (LGPD).
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" onClick={toggleOptOuts}>
            Descadastrados
          </Button>
          <Button type="button" variant="outline" onClick={load} disabled={loading} className="gap-2">
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Atualizar
          </Button>
          <Button type="button" onClick={() => setShowForm((v) => !v)} className="gap-2">
            <Plus className="h-4 w-4" />
            Nova campanha
          </Button>
        </div>
      </div>

      {optOuts && (
        <div className="mb-4 rounded-md border border-border p-3">
          <div className="text-sm font-medium text-foreground mb-2">Descadastrados ({optOuts.length})</div>
          {optOuts.length === 0 ? (
            <p className="text-xs text-muted-foreground">Ninguém pediu descadastro.</p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {optOuts.map((o) => (
                <span key={o.number} className="flex items-center gap-1 rounded bg-muted px-2 py-0.5 text-xs" title={o.reason}>
                  {o.number}
                  <button
                    type="button"
                    className="text-red-500"
                    title="Remover do descadastro"
                    onClick={async () => {
                      if (!confirm(`Voltar a enviar campanhas para ${o.number}?`)) return;
                      await campaignApi.removeOptOut(instanceId, instanceToken, o.number);
                      setOptOuts(await campaignApi.listOptOuts(instanceId, instanceToken));
                    }}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      {showForm && (
        <div className="grid gap-3 md:grid-cols-2 mb-6 rounded-md border border-border p-4">
          <div>
            <label className="text-sm font-medium text-foreground">Nome</label>
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
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
                <input className={inputClass} value={mediaUrl} onChange={(e) => setMediaUrl(e.target.value)} />
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
            <label className="text-sm font-medium text-foreground">
              {type === "text" ? "Mensagem" : "Legenda"} (use {"{{name}}"} para o nome)
            </label>
            <textarea className={inputClass} rows={4} value={text} onChange={(e) => setText(e.target.value)} />
          </div>
          <div className="md:col-span-2">
            <label className="text-sm font-medium text-foreground">
              Destinatários — um por linha: <code>número</code> ou <code>número;nome</code> ({parsed.length})
            </label>
            <textarea
              className={`${inputClass} font-mono`}
              rows={6}
              value={numbers}
              onChange={(e) => setNumbers(e.target.value)}
              placeholder={"5511999990001;Ana\n5511999990002"}
            />
          </div>
          <div>
            <label className="text-sm font-medium text-foreground">Intervalo mínimo entre envios (s)</label>
            <input type="number" min={0} className={inputClass} value={delayMin} onChange={(e) => setDelayMin(Number(e.target.value))} />
          </div>
          <div>
            <label className="text-sm font-medium text-foreground">Intervalo máximo entre envios (s)</label>
            <input type="number" min={0} className={inputClass} value={delayMax} onChange={(e) => setDelayMax(Number(e.target.value))} />
          </div>
          <label className="flex items-center gap-2 text-sm text-foreground">
            <input type="checkbox" checked={startNow} onChange={(e) => setStartNow(e.target.checked)} />
            Iniciar o envio agora (senão fica como rascunho)
          </label>
          <label className="flex items-center gap-2 text-sm text-foreground">
            <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
            Os contatos autorizaram receber mensagens desta empresa
          </label>
          <div className="md:col-span-2 flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setShowForm(false)}>
              Cancelar
            </Button>
            <Button type="button" onClick={create} disabled={saving} className="gap-2">
              <Megaphone className="h-4 w-4" />
              {saving ? "Criando..." : "Criar campanha"}
            </Button>
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground py-6 text-center">
          {loading ? "Carregando..." : "Nenhuma campanha."}
        </p>
      ) : (
        <div className="space-y-3">
          {items.map((c) => {
            const done = c.counts.sent + c.counts.failed + c.counts.skipped;
            const pct = c.total ? Math.round((done / c.total) * 100) : 0;
            return (
              <div key={c.id} className="rounded-md border border-border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <button
                    type="button"
                    className="text-left"
                    onClick={() => setExpanded(expanded === c.id ? null : c.id)}
                  >
                    <div className="font-medium text-foreground">{c.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {new Date(c.createdAt).toLocaleString("pt-BR")} · {c.total} destinatários · intervalo{" "}
                      {c.delayMinMs / 1000}–{c.delayMaxMs / 1000}s
                    </div>
                  </button>
                  <div className="flex items-center gap-2">
                    <span className={`rounded px-2 py-0.5 text-xs ${statusStyle[c.status] || ""}`}>
                      {statusLabel[c.status] || c.status}
                    </span>
                    {(c.status === "draft" || c.status === "paused") && (
                      <Button type="button" variant="outline" className={iconBtn} title="Iniciar" disabled={busyId === c.id} onClick={() => act(c, "start")}>
                        <Play className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {c.status === "running" && (
                      <Button type="button" variant="outline" className={iconBtn} title="Pausar" disabled={busyId === c.id} onClick={() => act(c, "pause")}>
                        <Pause className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {c.counts.failed > 0 && c.status !== "canceled" && (
                      <Button type="button" variant="outline" className={iconBtn} title="Reenviar falhas" disabled={busyId === c.id} onClick={() => act(c, "retry")}>
                        <RotateCcw className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {["draft", "running", "paused"].includes(c.status) && (
                      <Button type="button" variant="outline" className={iconBtn} title="Cancelar" disabled={busyId === c.id} onClick={() => act(c, "cancel")}>
                        <Square className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {c.status !== "running" && (
                      <Button type="button" variant="outline" className={iconBtn} title="Apagar" disabled={busyId === c.id} onClick={() => remove(c)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                </div>
                <div className="mt-2 h-2 w-full rounded bg-muted overflow-hidden">
                  <div className="h-2 bg-green-500" style={{ width: `${pct}%` }} />
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {pct}% · enviadas {c.counts.sent} · falhas {c.counts.failed} · puladas {c.counts.skipped} · na fila{" "}
                  {c.counts.pending + c.counts.sending}
                </div>
                {c.lastError && <div className="mt-1 text-xs text-red-500">{c.lastError}</div>}

                {expanded === c.id && (
                  <div className="mt-3">
                    <select
                      className="mb-2 rounded-md border border-input bg-background px-2 py-1 text-sm text-foreground"
                      value={recipientFilter}
                      onChange={(e) => setRecipientFilter(e.target.value)}
                    >
                      <option value="">Todos</option>
                      <option value="pending">Na fila</option>
                      <option value="sent">Enviadas</option>
                      <option value="failed">Falhas</option>
                      <option value="skipped">Puladas</option>
                    </select>
                    <div className="max-h-72 overflow-auto">
                      <table className="w-full text-sm">
                        <tbody>
                          {recipients.map((r) => (
                            <tr key={r.id} className="border-b border-border/50 align-top">
                              <td className="py-1 pr-3">{r.number}</td>
                              <td className="py-1 pr-3 text-muted-foreground">{r.name}</td>
                              <td className="py-1 pr-3">
                                <span className={`rounded px-2 py-0.5 text-xs ${statusStyle[r.status] || ""}`}>
                                  {statusLabel[r.status] || r.status}
                                </span>
                              </td>
                              <td className="py-1 pr-3 text-xs text-muted-foreground whitespace-nowrap">
                                {r.sentAt ? new Date(r.sentAt).toLocaleString("pt-BR") : ""}
                              </td>
                              <td className="py-1 text-xs text-red-500 break-words">{r.error}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                      {recipients.length === 0 && <p className="text-xs text-muted-foreground py-2">Nenhum destinatário.</p>}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
