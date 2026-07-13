import { create } from 'zustand';
import { fetchBranding, DEFAULT_BRANDING, type Branding } from '@/services/api/branding';

interface BrandingStore {
  branding: Branding;
  loaded: boolean;
  load: () => Promise<void>;
  refresh: () => Promise<void>;
}

const applyToDocument = (branding: Branding) => {
  document.title = branding.appName;
  if (branding.logo) {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = branding.logo;
  }
};

const useBrandingStore = create<BrandingStore>()((set, get) => ({
  branding: DEFAULT_BRANDING,
  loaded: false,
  load: async () => {
    if (get().loaded) return;
    const branding = await fetchBranding();
    set({ branding, loaded: true });
    applyToDocument(branding);
  },
  // Ignora o guard `loaded` — usado depois de salvar branding no Admin,
  // pra refletir nome/logo novos na sidebar/título sem precisar recarregar.
  refresh: async () => {
    const branding = await fetchBranding();
    set({ branding, loaded: true });
    applyToDocument(branding);
  },
}));

export default useBrandingStore;
