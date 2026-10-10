/**
 * Histórico de mensagens da instância (busca, conversas e status de entrega).
 * Rotas autenticadas com o token da própria instância (header apikey).
 */

import apiClient from './client';

export interface HistoryMessage {
  id: string;
  messageId: string;
  chatJid: string;
  senderJid?: string;
  fromMe: boolean;
  isGroup: boolean;
  pushName?: string;
  type: string;
  content?: string;
  mediaUrl?: string;
  mediaMime?: string;
  fileName?: string;
  quotedId?: string;
  status: 'received' | 'sent' | 'delivered' | 'read' | 'played';
  source: 'whatsapp' | 'api';
  edited: boolean;
  deleted: boolean;
  timestamp: string;
}

export interface HistoryChat {
  chatJid: string;
  isGroup: boolean;
  pushName?: string;
  lastType: string;
  lastContent?: string;
  lastFromMe: boolean;
  lastAt: string;
  messages: number;
}

export interface HistoryQuery {
  chat?: string;
  q?: string;
  type?: string;
  fromMe?: boolean;
  before?: string;
  limit?: number;
}

const auth = (token: string) => ({ headers: { apikey: token } });

export const searchHistory = async (
  instanceId: string,
  token: string,
  query: HistoryQuery
): Promise<{ messages: HistoryMessage[]; nextBefore?: string }> => {
  const params = new URLSearchParams();
  Object.entries(query).forEach(([k, v]) => {
    if (v !== undefined && v !== '') params.set(k, String(v));
  });
  const r = await apiClient.get<{ messages: HistoryMessage[]; nextBefore?: string }>(
    `/instance/history/${instanceId}?${params.toString()}`,
    auth(token)
  );
  return { messages: r.data.messages || [], nextBefore: r.data.nextBefore };
};

export const listHistoryChats = async (instanceId: string, token: string): Promise<HistoryChat[]> => {
  const r = await apiClient.get<{ chats: HistoryChat[] }>(`/instance/history/${instanceId}/chats`, auth(token));
  return r.data.chats || [];
};
