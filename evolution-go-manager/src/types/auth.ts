export type LicenseState = 'unchecked' | 'licensed' | 'unlicensed' | 'pending';
export type AuthMode = 'session' | 'legacy' | null;

export interface AccessGroup {
  id: string;
  name: string;
  ldapGroupDn: string;
  createdAt: string;
}

export interface AccessUser {
  id: string;
  username: string;
  displayName: string;
  role: 'admin' | 'user';
  authSource: 'local' | 'ldap';
  mustChangePassword: boolean;
  createdAt: string;
  groups?: AccessGroup[];
}

export interface AuthState {
  apiUrl: string;
  apiKey: string;
  token: string;
  user: AccessUser | null;
  authMode: AuthMode;
  isAuthenticated: boolean;
  licenseState: LicenseState;
}

export interface AuthStore extends AuthState {
  login: (apiUrl: string, apiKey: string) => Promise<void>;
  loginWithPassword: (apiUrl: string, username: string, password: string) => Promise<AccessUser>;
  refreshUser: () => Promise<AccessUser>;
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>;
  logout: () => Promise<void>;
  setApiUrl: (apiUrl: string) => void;
  setApiKey: (apiKey: string) => void;
  setLicenseState: (state: LicenseState) => void;
  checkLicense: (apiUrl?: string, apiKey?: string) => Promise<LicenseState>;
}

export interface LoginResponse {
  token: string;
  user: AccessUser;
}
