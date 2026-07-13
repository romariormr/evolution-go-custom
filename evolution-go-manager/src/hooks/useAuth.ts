import useAuthStore from '@/store/authStore';

/**
 * useAuth Hook
 *
 * Convenient hook to access auth store
 * Provides authentication state and methods
 */
function useAuth() {
  const authStore = useAuthStore();

  return {
    // State
    isAuthenticated: authStore.isAuthenticated,
    apiUrl: authStore.apiUrl,
    apiKey: authStore.apiKey,
    token: authStore.token,
    user: authStore.user,
    authMode: authStore.authMode,
    licenseState: authStore.licenseState,

    // Methods
    login: authStore.login,
    loginWithPassword: authStore.loginWithPassword,
    refreshUser: authStore.refreshUser,
    changePassword: authStore.changePassword,
    logout: authStore.logout,
    setApiUrl: authStore.setApiUrl,
    setApiKey: authStore.setApiKey,
    setLicenseState: authStore.setLicenseState,
    checkLicense: authStore.checkLicense,
  };
}

export default useAuth;
