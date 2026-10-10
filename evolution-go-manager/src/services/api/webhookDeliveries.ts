/**
 * Log de entregas de webhook da instância.
 * Rotas autenticadas com o token da própria instância (header apikey).
 */

import apiClient from './client';

export interface WebhookDelivery {
  id: string;
  instanceId: string;
  url: string;
  event: string;
  status: 'pending' | 'success' | 'failed';
  attempts: number;
  httpStatus: number;
  lastError: string;
  response: string;
  latencyMs: number;
  createdAt: string;
  updatedAt: string;
}

export interface WebhookTestResult {
  url: string;
  ok: boolean;
  httpStatus: number;
  latencyMs: number;
  error?: string;
  response?: string;
}

/** GET /instance/webhooks/:instanceId/deliveries */
export const listWebhookDeliveries = async (
  instanceId: string,
  instanceToken: string,
  status?: string,
  limit = 100
): Promise<WebhookDelivery[]> => {
  const params = new URLSearchParams({ limit: String(limit) });
  if (status) params.set('status', status);
  const response = await apiClient.get<{ deliveries: WebhookDelivery[] }>(
    `/instance/webhooks/${instanceId}/deliveries?${params.toString()}`,
    { headers: { apikey: instanceToken } }
  );
  return response.data.deliveries || [];
};

/** POST /instance/webhooks/:instanceId/deliveries/:deliveryId/resend */
export const resendWebhookDelivery = async (
  instanceId: string,
  instanceToken: string,
  deliveryId: string
): Promise<WebhookDelivery> => {
  const response = await apiClient.post<{ delivery: WebhookDelivery }>(
    `/instance/webhooks/${instanceId}/deliveries/${deliveryId}/resend`,
    {},
    { headers: { apikey: instanceToken } }
  );
  return response.data.delivery;
};

/** POST /instance/webhooks/:instanceId/test */
export const testWebhook = async (
  instanceId: string,
  instanceToken: string
): Promise<WebhookTestResult[]> => {
  const response = await apiClient.post<{ results: WebhookTestResult[] }>(
    `/instance/webhooks/${instanceId}/test`,
    {},
    { headers: { apikey: instanceToken } }
  );
  return response.data.results || [];
};
