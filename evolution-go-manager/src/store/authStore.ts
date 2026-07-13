import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import apiClient from '@/services/api/client';
import { checkLicenseStatus } from '@/services/api/license';
import type { AccessUser, AuthStore, LicenseState, LoginResponse } from '@/types/auth';

const defaultUrl = () =>
  typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8082';

const useAuthStore = create<AuthStore>()(
  persist(
    (set, get) => ({
      apiUrl: defaultUrl(),
      apiKey: '',
      token: '',
      user: null,
      authMode: null,
      isAuthenticated: false,
      licenseState: 'unchecked' as LicenseState,

      loginWithPassword: async (apiUrl, username, password) => {
        const cleanUrl = apiUrl.replace(/\/$/, '');
        const response = await apiClient.post<LoginResponse>(
          '/access/login',
          { username, password },
          { baseURL: cleanUrl, withCredentials: true },
        );
        const { token, user } = response.data;
        set({
          apiUrl: cleanUrl,
          apiKey: '',
          token,
          user,
          authMode: 'session',
          isAuthenticated: true,
          licenseState: 'licensed',
        });
        return user;
      },

      refreshUser: async () => {
        const response = await apiClient.get<AccessUser>('/access/me');
        set({ user: response.data, isAuthenticated: true });
        return response.data;
      },

      changePassword: async (currentPassword, newPassword) => {
        await apiClient.post('/access/me/password', { currentPassword, newPassword });
        const user = get().user;
        if (user) set({ user: { ...user, mustChangePassword: false } });
      },

      login: async (apiUrl, apiKey) => {
        const cleanUrl = apiUrl.replace(/\/$/, '');
        try {
          await apiClient.get('/instance/all', {
            baseURL: cleanUrl,
            headers: { apikey: apiKey, 'Cache-Control': 'no-cache' },
            params: { t: Date.now() },
          });
          set({
            apiUrl: cleanUrl,
            apiKey,
            token: '',
            user: null,
            authMode: 'legacy',
            isAuthenticated: true,
          });
        } catch (error: unknown) {
          set({ isAuthenticated: false });
          const status = (error as { status?: number; response?: { status?: number } })?.status ??
            (error as { response?: { status?: number } })?.response?.status;
          if (status === 401 || status === 403) {
            throw new Error('API Key inválida. Verifique a chave informada.');
          }
          throw new Error('Não foi possível conectar. Verifique a URL e a API Key.');
        }
      },

      logout: async () => {
        if (get().authMode === 'session') {
          try { await apiClient.post('/access/logout'); } catch { /* local logout still applies */ }
        }
        localStorage.removeItem('evolution-auth');
        set({
          apiUrl: defaultUrl(), apiKey: '', token: '', user: null, authMode: null,
          isAuthenticated: false, licenseState: 'unchecked',
        });
        window.location.href = '/manager/login';
      },

      setApiUrl: (apiUrl) => set({ apiUrl: apiUrl.replace(/\/$/, '') }),
      setApiKey: (apiKey) => set({ apiKey }),
      setLicenseState: (licenseState) => set({ licenseState }),
      checkLicense: async (apiUrl, apiKey) => {
        try {
          const result = await checkLicenseStatus(apiUrl, apiKey);
          const state: LicenseState = result.status === 'active' ? 'licensed' : 'unlicensed';
          set({ licenseState: state });
          return state;
        } catch {
          set({ licenseState: 'unlicensed' });
          return 'unlicensed';
        }
      },
    }),
    {
      name: 'evolution-auth',
      partialize: ({ apiUrl, apiKey, token, user, authMode, isAuthenticated, licenseState }) =>
        ({ apiUrl, apiKey, token, user, authMode, isAuthenticated, licenseState }),
    },
  ),
);

export default useAuthStore;
