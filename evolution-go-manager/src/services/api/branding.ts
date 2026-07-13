import axios from 'axios';

export interface Branding {
  appName: string;
  logo: string;
}

const DEFAULT_BRANDING: Branding = { appName: 'Evolution GO', logo: '' };

// Endpoint público — sem apikey/Authorization. Usa axios puro (não o apiClient
// autenticado) para funcionar também na tela de login, antes de qualquer sessão.
export async function fetchBranding(): Promise<Branding> {
  try {
    const response = await axios.get<Branding>('/access/branding', {
      baseURL: window.location.origin,
      timeout: 5000,
    });
    return { ...DEFAULT_BRANDING, ...response.data };
  } catch {
    return DEFAULT_BRANDING;
  }
}

export { DEFAULT_BRANDING };
