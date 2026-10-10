/**
 * Campanhas (disparo em massa com acompanhamento por destinatário) e lista de
 * descadastro da instância. Rotas autenticadas com o token da própria instância.
 */

import apiClient from './client';

export type CampaignStatus = 'draft' | 'running' | 'paused' | 'completed' | 'canceled';
export type RecipientStatus = 'pending' | 'sending' | 'sent' | 'failed' | 'skipped';

export interface CampaignCounts {
  pending: number;
  sending: number;
  sent: number;
  failed: number;
  skipped: number;
}

export interface Campaign {
  id: string;
  instanceId: string;
  name: string;
  type: 'text' | 'media';
  message: string;
  status: CampaignStatus;
  startAt?: string;
  delayMinMs: number;
  delayMaxMs: number;
  total: number;
  startedAt?: string;
  finishedAt?: string;
  lastError?: string;
  createdAt: string;
  counts: CampaignCounts;
}

export interface CampaignRecipient {
  id: string;
  number: string;
  name: string;
  status: RecipientStatus;
  attempts: number;
  messageId?: string;
  error?: string;
  sentAt?: string;
}

export interface CampaignMessage {
  text?: string;
  mediaUrl?: string;
  mediaType?: string;
  caption?: string;
  filename?: string;
}

export interface CreateCampaignPayload {
  name: string;
  type: 'text' | 'media';
  message: CampaignMessage;
  recipients: { number: string; name?: string }[];
  startAt?: string;
  delayMinMs?: number;
  delayMaxMs?: number;
  start?: boolean;
}

const auth = (instanceToken: string) => ({ headers: { apikey: instanceToken } });
const base = (instanceId: string) => `/instance/campaign/${instanceId}`;

export const listCampaigns = async (instanceId: string, token: string): Promise<Campaign[]> => {
  const r = await apiClient.get<{ campaigns: Campaign[] }>(base(instanceId), auth(token));
  return r.data.campaigns || [];
};

export const createCampaign = async (instanceId: string, token: string, data: CreateCampaignPayload): Promise<Campaign> => {
  const r = await apiClient.post<{ campaign: Campaign }>(base(instanceId), data, auth(token));
  return r.data.campaign;
};

export const campaignAction = async (
  instanceId: string,
  token: string,
  id: string,
  action: 'start' | 'pause' | 'cancel' | 'retry'
): Promise<Campaign> => {
  const r = await apiClient.post<{ campaign: Campaign }>(`${base(instanceId)}/${id}/${action}`, {}, auth(token));
  return r.data.campaign;
};

export const deleteCampaign = async (instanceId: string, token: string, id: string): Promise<void> => {
  await apiClient.delete(`${base(instanceId)}/${id}`, auth(token));
};

export const listCampaignRecipients = async (
  instanceId: string,
  token: string,
  id: string,
  status?: string
): Promise<CampaignRecipient[]> => {
  const params = new URLSearchParams({ limit: '500' });
  if (status) params.set('status', status);
  const r = await apiClient.get<{ recipients: CampaignRecipient[] }>(
    `${base(instanceId)}/${id}/recipients?${params.toString()}`,
    auth(token)
  );
  return r.data.recipients || [];
};

export interface OptOut {
  number: string;
  reason: string;
  createdAt: string;
}

export const listOptOuts = async (instanceId: string, token: string): Promise<OptOut[]> => {
  const r = await apiClient.get<{ optOuts: OptOut[] }>(`/instance/optout/${instanceId}`, auth(token));
  return r.data.optOuts || [];
};

export const removeOptOut = async (instanceId: string, token: string, number: string): Promise<void> => {
  await apiClient.delete(`/instance/optout/${instanceId}/${encodeURIComponent(number)}`, auth(token));
};
