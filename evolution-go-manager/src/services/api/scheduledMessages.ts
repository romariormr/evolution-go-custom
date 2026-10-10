/**
 * Mensagens agendadas da instância (envio único ou recorrente via cron).
 * Rotas autenticadas com o token da própria instância (header apikey).
 */

import apiClient from './client';

export type ScheduleStatus = 'scheduled' | 'sending' | 'sent' | 'failed' | 'paused';

export interface ScheduledMessage {
  id: string;
  instanceId: string;
  type: string;
  number: string;
  description: string;
  payload: string;
  sendAt?: string;
  cronExpr?: string;
  timezone: string;
  nextRunAt?: string;
  status: ScheduleStatus;
  runCount: number;
  lastRunAt?: string;
  lastError?: string;
  lastMessageId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateSchedulePayload {
  type: 'text' | 'link' | 'media' | 'poll' | 'location' | 'contact';
  description?: string;
  sendAt?: string;
  cron?: string;
  timezone?: string;
  payload: Record<string, unknown>;
}

const auth = (instanceToken: string) => ({ headers: { apikey: instanceToken } });

/** GET /instance/schedule/:instanceId */
export const listSchedules = async (
  instanceId: string,
  instanceToken: string,
  status?: string
): Promise<ScheduledMessage[]> => {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  const qs = params.toString();
  const response = await apiClient.get<{ schedules: ScheduledMessage[] }>(
    `/instance/schedule/${instanceId}${qs ? `?${qs}` : ''}`,
    auth(instanceToken)
  );
  return response.data.schedules || [];
};

/** POST /instance/schedule/:instanceId */
export const createSchedule = async (
  instanceId: string,
  instanceToken: string,
  data: CreateSchedulePayload
): Promise<ScheduledMessage> => {
  const response = await apiClient.post<{ schedule: ScheduledMessage }>(
    `/instance/schedule/${instanceId}`,
    data,
    auth(instanceToken)
  );
  return response.data.schedule;
};

/** DELETE /instance/schedule/:instanceId/:scheduleId */
export const deleteSchedule = async (instanceId: string, instanceToken: string, id: string): Promise<void> => {
  await apiClient.delete(`/instance/schedule/${instanceId}/${id}`, auth(instanceToken));
};

/** POST /instance/schedule/:instanceId/:scheduleId/(pause|resume) */
export const setSchedulePaused = async (
  instanceId: string,
  instanceToken: string,
  id: string,
  paused: boolean
): Promise<ScheduledMessage> => {
  const response = await apiClient.post<{ schedule: ScheduledMessage }>(
    `/instance/schedule/${instanceId}/${id}/${paused ? 'pause' : 'resume'}`,
    {},
    auth(instanceToken)
  );
  return response.data.schedule;
};
