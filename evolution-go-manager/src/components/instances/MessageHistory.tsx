import { useCallback, useEffect, useState } from "react";
import { Check, CheckCheck, RefreshCw, Search } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import * as historyApi from "@/services/api/history";
import type { HistoryChat, HistoryMessage } from "@/services/api/history";

interface Props {
  instanceId: string;
  instanceToken: string;
}

const inputClass =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring";

const fmt = (iso: string) => new Date(iso).toLocaleString("pt-BR");
const shortJid = (jid: string) => jid.split("@")[0];

const typeLabel: Record<string, string> = {
  image: "📷 imagem",
  video: "🎬 vídeo",
  audio: "🎤 áudio",
  document: "📄 documento",
  sticker: "figurinha",
  location: "📍 localização",
  contact: "👤 contato",
  poll: "📊 enquete",
};

function StatusIcon({ m }: { m: HistoryMessage }) {
  if (!m.fromMe) return null;
  if (m.status === "read" || m.status === "played")
    return <CheckCheck className="inline h-3.5 w-3.5 text-blue-500" aria-label={m.status === "read" ? "lida" : "ouvida"} />;
  if (m.status === "delivered") return <CheckCheck className="inline h-3.5 w-3.5 text-muted-foreground" aria-label="entregue" />;
  return <Check className="inline h-3.5 w-3.5 text-muted-foreground" aria-label="enviada" />;
}

/**
 * Histórico de mensagens: conversas à esquerda, mensagens à direita, busca por
 * texto (full-text em português) e status de entrega das enviadas.
 */
export function MessageHistory({ instanceId, instanceToken }: Props) {
  const [chats, setChats] = useState<HistoryChat[]>([]);
  const [chat, setChat] = useState<string>("");
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [messages, setMessages] = useState<HistoryMessage[]>([]);
  const [nextBefore, setNextBefore] = useState<string | undefined>();
  const [loading, setLoading] = useState(false);

  const loadChats = useCallback(async () => {
    try {
      setChats(await historyApi.listHistoryChats(instanceId, instanceToken));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Erro ao carregar conversas");
    }
  }, [instanceId, instanceToken]);

  const loadMessages = useCallback(
    async (before?: string) => {
      setLoading(true);
      try {
        const r = await historyApi.searchHistory(instanceId, instanceToken, {
          chat: chat || undefined,
          q: query || undefined,
          before,
          limit: 100,
        });
        setMessages((prev) => (before ? [...prev, ...r.messages] : r.messages));
        setNextBefore(r.nextBefore);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Erro ao carregar mensagens");
      } finally {
        setLoading(false);
      }
    },
    [instanceId, instanceToken, chat, query]
  );

  useEffect(() => {
    loadChats();
  }, [loadChats]);

  useEffect(() => {
    loadMessages();
  }, [loadMessages]);

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Histórico de mensagens</h2>
          <p className="text-xs text-muted-foreground">
            Recebidas, enviadas pela API e pelo celular, com status de entrega. Mídias aparecem com link quando o
            armazenamento (MinIO) está ligado.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            loadChats();
            loadMessages();
          }}
          disabled={loading}
          className="gap-2"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Atualizar
        </Button>
      </div>

      <form
        className="flex gap-2 mb-4"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(q.trim());
        }}
      >
        <input className={inputClass} value={q} onChange={(e) => setQ(e.target.value)} placeholder="Buscar no texto das mensagens..." />
        <Button type="submit" className="gap-2">
          <Search className="h-4 w-4" />
          Buscar
        </Button>
      </form>

      <div className="grid gap-4 md:grid-cols-[260px_1fr]">
        <div className="max-h-[520px] overflow-auto rounded-md border border-border">
          <button
            type="button"
            onClick={() => setChat("")}
            className={`block w-full px-3 py-2 text-left text-sm border-b border-border ${chat === "" ? "bg-muted" : ""}`}
          >
            Todas as conversas
          </button>
          {chats.map((c) => (
            <button
              type="button"
              key={c.chatJid}
              onClick={() => setChat(c.chatJid)}
              className={`block w-full px-3 py-2 text-left border-b border-border/50 ${chat === c.chatJid ? "bg-muted" : ""}`}
            >
              <div className="flex justify-between gap-2 text-sm text-foreground">
                <span className="truncate">{c.isGroup ? "👥 " : ""}{c.pushName || shortJid(c.chatJid)}</span>
                <span className="text-xs text-muted-foreground">{c.messages}</span>
              </div>
              <div className="truncate text-xs text-muted-foreground">
                {c.lastFromMe ? "Você: " : ""}
                {c.lastContent || typeLabel[c.lastType] || c.lastType}
              </div>
            </button>
          ))}
          {chats.length === 0 && <p className="p-3 text-xs text-muted-foreground">Nenhuma conversa registrada ainda.</p>}
        </div>

        <div className="max-h-[520px] overflow-auto rounded-md border border-border p-3 space-y-2">
          {messages.length === 0 && (
            <p className="text-sm text-muted-foreground text-center py-6">{loading ? "Carregando..." : "Nenhuma mensagem."}</p>
          )}
          {messages.map((m) => (
            <div key={m.id} className={`flex ${m.fromMe ? "justify-end" : "justify-start"}`}>
              <div className={`max-w-[80%] rounded-lg px-3 py-2 text-sm ${m.fromMe ? "bg-green-500/15" : "bg-muted"}`}>
                <div className="text-xs text-muted-foreground mb-0.5">
                  {m.fromMe ? (m.source === "api" ? "API" : "Você") : m.pushName || shortJid(m.senderJid || m.chatJid)}
                  {!chat && <> · {shortJid(m.chatJid)}</>}
                </div>
                {m.deleted ? (
                  <i className="text-muted-foreground">mensagem apagada</i>
                ) : (
                  <>
                    {m.type !== "text" && (
                      <div className="text-xs">
                        {m.mediaUrl ? (
                          <a href={m.mediaUrl} target="_blank" rel="noreferrer" className="underline">
                            {typeLabel[m.type] || m.type} {m.fileName || ""}
                          </a>
                        ) : (
                          <span>{typeLabel[m.type] || m.type} {m.fileName || ""}</span>
                        )}
                      </div>
                    )}
                    {m.content && <div className="whitespace-pre-wrap break-words text-foreground">{m.content}</div>}
                  </>
                )}
                <div className="mt-0.5 text-right text-[11px] text-muted-foreground">
                  {m.edited && "editada · "}
                  {fmt(m.timestamp)} <StatusIcon m={m} />
                </div>
              </div>
            </div>
          ))}
          {nextBefore && (
            <div className="text-center">
              <Button type="button" variant="outline" onClick={() => loadMessages(nextBefore)} disabled={loading}>
                Carregar mais antigas
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
