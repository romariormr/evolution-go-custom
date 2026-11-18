import apiClient from './client';
import type { AccessAudit, AccessGroup, AccessSession, AccessUser } from '@/types/auth';
import type { Instance, RawInstance } from '@/types/instance';

const data = <T>(response: { data: { data: T } }) => response.data.data;

export const listUsers = async () => data<AccessUser[]>(await apiClient.get('/access/admin/users'));
export const createUser = async (payload: {
  username: string; email: string; password: string; displayName: string; role: string;
  seeAllInstances: boolean; groupIds: string[];
}) => data<AccessUser>(await apiClient.post('/access/admin/users', payload));
export const updateUser = async (userId: string, payload: {
  displayName?: string; email?: string; role?: string; active?: boolean; seeAllInstances?: boolean;
}) => data<AccessUser>(await apiClient.put(`/access/admin/users/${userId}`, payload));
export const resetUserTotp = (userId: string) => apiClient.post(`/access/admin/users/${userId}/2fa/reset`);
export const listUserSessions = async (userId: string) =>
  data<AccessSession[]>(await apiClient.get(`/access/admin/users/${userId}/sessions`));
export const revokeSession = (sessionId: string) => apiClient.delete(`/access/admin/sessions/${sessionId}`);
export const instanceGroups = async (instanceId: string) =>
  data<string[]>(await apiClient.get(`/access/admin/instances/${instanceId}/groups`));
export const listAudit = async (params: { username?: string; action?: string; limit?: number; offset?: number }) => {
  const response = await apiClient.get<{ data: AccessAudit[]; total: number }>('/access/admin/audit', { params });
  return response.data;
};
export const testLdapUser = async (username: string, password: string) =>
  data<{ dn: string; displayName: string; email: string; memberOf: string[]; matchedGroups: string[]; wouldBeAdmin: boolean }>(
    await apiClient.post('/access/admin/settings/ldap/test-user', { username, password }));

// Minha conta
export const mySessions = async () => data<AccessSession[]>(await apiClient.get('/access/me/sessions'));
export const revokeMySession = (sessionId: string) => apiClient.delete(`/access/me/sessions/${sessionId}`);
export const startTotpSetup = async () =>
  (await apiClient.post<{ secret: string; otpauthUrl: string; qrCode: string }>('/access/me/2fa/setup')).data;
export const enableTotp = (code: string) => apiClient.post('/access/me/2fa/enable', { code });
export const disableTotp = (password: string, code: string) => apiClient.post('/access/me/2fa/disable', { password, code });
export const setUserGroups = (userId: string, groupIds: string[]) =>
  apiClient.put(`/access/admin/users/${userId}/groups`, { groupIds });
export const resetUserPassword = (userId: string, password: string) =>
  apiClient.put(`/access/admin/users/${userId}/password`, { password });
export const deleteUser = (userId: string) => apiClient.delete(`/access/admin/users/${userId}`);

export const listGroups = async () => data<AccessGroup[]>(await apiClient.get('/access/admin/groups'));
export const createGroup = async (payload: { name: string; ldapGroupDn: string }) =>
  data<AccessGroup>(await apiClient.post('/access/admin/groups', payload));
export const deleteGroup = (groupId: string) => apiClient.delete(`/access/admin/groups/${groupId}`);
export const regenerateGroupApiKey = async (groupId: string) =>
  data<AccessGroup>(await apiClient.post(`/access/admin/groups/${groupId}/api-key/regenerate`));
export const linkInstance = (groupId: string, instanceId: string) =>
  apiClient.post(`/access/admin/groups/${groupId}/instances/${instanceId}`);
export const unlinkInstance = (groupId: string, instanceId: string) =>
  apiClient.delete(`/access/admin/groups/${groupId}/instances/${instanceId}`);

export const listSettings = async () => data<Record<string, string>>(await apiClient.get('/access/admin/settings'));
export const setSetting = (key: string, value: string) =>
  apiClient.put(`/access/admin/settings/${encodeURIComponent(key)}`, { value });
export const testLdap = () => apiClient.post('/access/admin/settings/ldap/test');

export const uploadLogo = async (file: File): Promise<string> => {
  const form = new FormData();
  form.append('logo', file);
  const response = await apiClient.post<{ url: string }>('/access/admin/branding/logo', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return response.data.url;
};

export const normalizeAccessInstance = (raw: RawInstance): Instance => ({
  id: raw.id,
  instanceName: raw.name,
  status: raw.connected ? 'open' : 'close',
  apikey: raw.token,
  owner: raw.jid ? raw.jid.split('@')[0] : '',
  profileName: raw.name,
  connected: raw.connected,
  webhook: raw.webhook || undefined,
  createdAt: raw.createdAt,
  disconnectReason: raw.disconnect_reason || undefined,
  alwaysOnline: raw.alwaysOnline,
  rejectCall: raw.rejectCall,
  readMessages: raw.readMessages,
  ignoreGroups: raw.ignoreGroups,
  ignoreStatus: raw.ignoreStatus,
});
