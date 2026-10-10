import { useCallback, useEffect, useState } from "react";
import { Plus, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import * as autoReplyApi from "@/services/api/autoReply";
import type { AutoReplyMatch, AutoReplyRule, AutoReplyTrigger } from "@/services/api/autoReply";

interface Props {
  instanceId: string;
  instanceToken: string;
}

const inputClass =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring";

const triggerLabel: Record<AutoReplyTrigger, string> = {
  keyword: "Palavra-chave",
  away: "Fora do horário",
  welcome: "Boas-vindas",
};

const weekDays = ["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"];

const emptyRule = (): AutoReplyRule => ({
  name: "",
  enabled: true,
  trigger: "keyword",
  matchType: "contains",
  pattern: "",
  response: "",
  cooldownMinutes: 0,
  skipIfAgent: true,
  days: "1,2,3,4,5",
  startTime: "08:00",
  endTime: "18:00",
});

const describe = (r: AutoReplyRule) => {
  if (r.trigger === "keyword") return `${r.matchType}: "${r.pattern}"`;
  if (r.trigger === "away") {
    const days = (r.days || "")
      .split(",")
      .filter(Boolean)
      .map((d) => weekDays[Number(d)])
      .join(", ");
    return `atendimento ${days} ${r.startTime}–${r.endTime}`;
  }
  return "primeira mensagem do contato";
};

/**
 * Respostas automáticas: palavra-chave (contains/equals/startsWith/regex),
 * fora do horário de atendimento e boas-vindas, com cooldown por contato e
 * opção de não responder quando a conversa já está com um agente no Chatwoot.
 */
export function AutoReplyRules({ instanceId, instanceToken }: Props) {
  const [items, setItems] = useState<AutoReplyRule[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [draft, setDraft] = useState<AutoReplyRule>(emptyRule());
  const [showForm, setShowForm] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await autoReplyApi.listAutoReplies(instanceId, instanceToken));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar regras");
    } finally {
      setLoading(false);
    }
  }, [instanceId, instanceToken]);

  useEffect(() => {
    load();
  }, [load]);

  const set = <K extends keyof AutoReplyRule>(key: K, value: AutoReplyRule[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const toggleDay = (day: number) => {
    const days = new Set((draft.days || "").split(",").filter(Boolean).map(Number));
    if (days.has(day)) days.delete(day);
    else days.add(day);
    set("days", [...days].sort().join(","));
  };

  const create = async () => {
    if (!draft.response.trim()) return toast.error("Informe a resposta");
    if (draft.trigger === "keyword" && !draft.pattern?.trim()) return toast.error("Informe a palavra-chave");
    setSaving(true);
    try {
      await autoReplyApi.createAutoReply(instanceId, instanceToken, {
        ...draft,
        name: draft.name.trim() || triggerLabel[draft.trigger],
        cooldownMinutes: Number(draft.cooldownMinutes) || 0,
      });
      toast.success("Regra criada");
      setDraft(emptyRule());
      setShowForm(false);
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao criar regra");
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = async (r: AutoReplyRule) => {
    setBusyId(r.id!);
    try {
      await autoReplyApi.updateAutoReply(instanceId, instanceToken, { ...r, enabled: !r.enabled });
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao atualizar regra");
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (r: AutoReplyRule) => {
    if (!confirm(`Apagar a regra "${r.name}"?`)) return;
    setBusyId(r.id!);
    try {
      await autoReplyApi.deleteAutoReply(instanceId, instanceToken, r.id!);
      toast.success("Regra apagada");
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao apagar regra");
    } finally {
      setBusyId(null);
    }
  };

  const selectedDays = new Set((draft.days || "").split(",").filter(Boolean).map(Number));

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Respostas automáticas</h2>
          <p className="text-xs text-muted-foreground">
            Uma resposta por mensagem (a regra de menor prioridade que casar). Grupos e mensagens antigas são ignorados.
            Use {"{{name}}"} para o nome do contato.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" onClick={load} disabled={loading} className="gap-2">
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Atualizar
          </Button>
          <Button type="button" onClick={() => setShowForm((v) => !v)} className="gap-2">
            <Plus className="h-4 w-4" />
            Nova regra
          </Button>
        </div>
      </div>

      {showForm && (
        <div className="grid gap-3 md:grid-cols-2 mb-6 rounded-md border border-border p-4">
          <div>
            <label className="text-sm font-medium text-foreground">Gatilho</label>
            <select
              className={inputClass}
              value={draft.trigger}
              onChange={(e) => set("trigger", e.target.value as AutoReplyTrigger)}
            >
              <option value="keyword">Palavra-chave</option>
              <option value="away">Fora do horário de atendimento</option>
              <option value="welcome">Boas-vindas (1ª mensagem no período)</option>
            </select>
          </div>
          <div>
            <label className="text-sm font-medium text-foreground">Nome</label>
            <input className={inputClass} value={draft.name} onChange={(e) => set("name", e.target.value)} />
          </div>

          {draft.trigger === "keyword" && (
            <>
              <div>
                <label className="text-sm font-medium text-foreground">Tipo de comparação</label>
                <select
                  className={inputClass}
                  value={draft.matchType}
                  onChange={(e) => set("matchType", e.target.value as AutoReplyMatch)}
                >
                  <option value="contains">Contém</option>
                  <option value="equals">Igual</option>
                  <option value="startsWith">Começa com</option>
                  <option value="regex">Regex (RE2)</option>
                </select>
              </div>
              <div>
                <label className="text-sm font-medium text-foreground">Palavra-chave / padrão</label>
                <input className={inputClass} value={draft.pattern} onChange={(e) => set("pattern", e.target.value)} />
              </div>
            </>
          )}

          {draft.trigger === "away" && (
            <>
              <div className="md:col-span-2">
                <label className="text-sm font-medium text-foreground">Dias de atendimento</label>
                <div className="flex flex-wrap gap-2 mt-1">
                  {weekDays.map((d, i) => (
                    <label key={d} className="flex items-center gap-1 text-sm text-foreground">
                      <input type="checkbox" checked={selectedDays.has(i)} onChange={() => toggleDay(i)} />
                      {d}
                    </label>
                  ))}
                </div>
              </div>
              <div>
                <label className="text-sm font-medium text-foreground">Início do atendimento</label>
                <input type="time" className={inputClass} value={draft.startTime} onChange={(e) => set("startTime", e.target.value)} />
              </div>
              <div>
                <label className="text-sm font-medium text-foreground">Fim do atendimento</label>
                <input type="time" className={inputClass} value={draft.endTime} onChange={(e) => set("endTime", e.target.value)} />
              </div>
            </>
          )}

          <div className="md:col-span-2">
            <label className="text-sm font-medium text-foreground">Resposta</label>
            <textarea className={inputClass} rows={3} value={draft.response} onChange={(e) => set("response", e.target.value)} />
          </div>
          <div>
            <label className="text-sm font-medium text-foreground">Cooldown por contato (min, 0 = padrão)</label>
            <input
              type="number"
              min={0}
              className={inputClass}
              value={draft.cooldownMinutes}
              onChange={(e) => set("cooldownMinutes", Number(e.target.value))}
            />
          </div>
          <label className="flex items-center gap-2 text-sm text-foreground mt-6">
            <input type="checkbox" checked={draft.skipIfAgent} onChange={(e) => set("skipIfAgent", e.target.checked)} />
            Não responder se a conversa estiver com um agente no Chatwoot
          </label>
          <div className="md:col-span-2 flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setShowForm(false)}>
              Cancelar
            </Button>
            <Button type="button" onClick={create} disabled={saving}>
              {saving ? "Salvando..." : "Criar regra"}
            </Button>
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground py-6 text-center">
          {loading ? "Carregando..." : "Nenhuma regra cadastrada."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted-foreground border-b border-border">
                <th className="py-2 pr-3">Ativa</th>
                <th className="py-2 pr-3">Regra</th>
                <th className="py-2 pr-3">Quando</th>
                <th className="py-2 pr-3">Resposta</th>
                <th className="py-2 pr-3">Disparos</th>
                <th className="py-2"></th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id} className="border-b border-border/50 align-top">
                  <td className="py-2 pr-3">
                    <input
                      type="checkbox"
                      checked={r.enabled}
                      disabled={busyId === r.id}
                      onChange={() => toggleEnabled(r)}
                    />
                  </td>
                  <td className="py-2 pr-3">
                    <div className="text-foreground">{r.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {triggerLabel[r.trigger]} · prioridade {r.priority} · cooldown {r.cooldownMinutes} min
                    </div>
                  </td>
                  <td className="py-2 pr-3 text-xs text-muted-foreground">{describe(r)}</td>
                  <td className="py-2 pr-3 max-w-xs">
                    <div className="truncate" title={r.response}>
                      {r.response}
                    </div>
                  </td>
                  <td className="py-2 pr-3 whitespace-nowrap">
                    {r.matchCount || 0}
                    {r.lastMatchAt && (
                      <div className="text-xs text-muted-foreground">{new Date(r.lastMatchAt).toLocaleString("pt-BR")}</div>
                    )}
                  </td>
                  <td className="py-2">
                    <Button
                      type="button"
                      variant="outline"
                      className="h-8 px-2"
                      title="Apagar"
                      disabled={busyId === r.id}
                      onClick={() => remove(r)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
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
