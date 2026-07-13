import apiClient from './client';
import type { AccessGroup, AccessUser } from '@/types/auth';
import type { Instance, RawInstance } from '@/types/instance';

const data = <T>(response: { data: { data: T } }) => response.data.data;

export const listUsers = async () => data<AccessUser[]>(await apiClient.get('/access/admin/users'));
export const createUser = async (payload: {
  username: string; password: string; displayName: string; role: string; groupIds: string[];
}) => data<AccessUser>(await apiClient.post('/access/admin/users', payload));
export const setUserGroups = (userId: string, groupIds: string[]) =>
  apiClient.put(`/access/admin/users/${userId}/groups`, { groupIds });
export const resetUserPassword = (userId: string, password: string) =>
  apiClient.put(`/access/admin/users/${userId}/password`, { password });
export const deleteUser = (userId: string) => apiClient.delete(`/access/admin/users/${userId}`);

export const listGroups = async () => data<AccessGroup[]>(await apiClient.get('/access/admin/groups'));
export const createGroup = async (payload: { name: string; ldapGroupDn: string }) =>
  data<AccessGroup>(await apiClient.post('/access/admin/groups', payload));
export const deleteGroup = (groupId: string) => apiClient.delete(`/access/admin/groups/${groupId}`);
export const linkInstance = (groupId: string, instanceId: string) =>
  apiClient.post(`/access/admin/groups/${groupId}/instances/${instanceId}`);
export const unlinkInstance = (groupId: string, instanceId: string) =>
  apiClient.delete(`/access/admin/groups/${groupId}/instances/${instanceId}`);

export const listSettings = async () => data<Record<string, string>>(await apiClient.get('/access/admin/settings'));
export const setSetting = (key: string, value: string) =>
  apiClient.put(`/access/admin/settings/${encodeURIComponent(key)}`, { value });

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
