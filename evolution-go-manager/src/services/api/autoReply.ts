/**
 * Respostas automáticas da instância (palavra-chave, fora do horário, boas-vindas).
 * Rotas autenticadas com o token da própria instância (header apikey).
 */

import apiClient from './client';

export type AutoReplyTrigger = 'keyword' | 'away' | 'welcome';
export type AutoReplyMatch = 'contains' | 'equals' | 'startsWith' | 'regex';

export interface AutoReplyRule {
  id?: string;
  instanceId?: string;
  name: string;
  enabled: boolean;
  trigger: AutoReplyTrigger;
  matchType?: AutoReplyMatch;
  pattern?: string;
  response: string;
  priority?: number;
  cooldownMinutes?: number;
  skipIfAgent: boolean;
  timezone?: string;
  days?: string;
  startTime?: string;
  endTime?: string;
  matchCount?: number;
  lastMatchAt?: string;
}

const auth = (instanceToken: string) => ({ headers: { apikey: instanceToken } });

export const listAutoReplies = async (instanceId: string, instanceToken: string): Promise<AutoReplyRule[]> => {
  const response = await apiClient.get<{ rules: AutoReplyRule[] }>(`/instance/autoreply/${instanceId}`, auth(instanceToken));
  return response.data.rules || [];
};

export const createAutoReply = async (
  instanceId: string,
  instanceToken: string,
  rule: AutoReplyRule
): Promise<AutoReplyRule> => {
  const response = await apiClient.post<{ rule: AutoReplyRule }>(`/instance/autoreply/${instanceId}`, rule, auth(instanceToken));
  return response.data.rule;
};

export const updateAutoReply = async (
  instanceId: string,
  instanceToken: string,
  rule: AutoReplyRule
): Promise<AutoReplyRule> => {
  const response = await apiClient.put<{ rule: AutoReplyRule }>(
    `/instance/autoreply/${instanceId}/${rule.id}`,
    rule,
    auth(instanceToken)
  );
  return response.data.rule;
};

export const deleteAutoReply = async (instanceId: string, instanceToken: string, id: string): Promise<void> => {
  await apiClient.delete(`/instance/autoreply/${instanceId}/${id}`, auth(instanceToken));
};
