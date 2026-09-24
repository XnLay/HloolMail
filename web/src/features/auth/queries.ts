import { useQuery } from '@tanstack/react-query';
import { api } from '../../api';
import { queryKeys } from '../../lib/queryKeys';
import type { PublicLoginSettings } from '../../types';

export function usePublicLoginSettings() {
  const loginSettings = useQuery({
    queryKey: queryKeys.loginSettings,
    queryFn: () => api<PublicLoginSettings>('/api/auth/login-settings'),
    retry: false,
    staleTime: 60000,
  });
  const oauthProviderRows = loginSettings.data?.oauth_providers ?? [];
  const registrationOpen = loginSettings.data?.registration_open === true;
  const emailRegistrationAvailable =
    loginSettings.isSuccess &&
    registrationOpen &&
    loginSettings.data.email_registration_enabled === true;
  const oauthRegistrationAvailable =
    loginSettings.isSuccess && registrationOpen && oauthProviderRows.length > 0;
  const registrationAvailable = emailRegistrationAvailable || oauthRegistrationAvailable;
  return {
    loginSettings,
    oauthProviderRows,
    registrationOpen,
    emailRegistrationAvailable,
    oauthRegistrationAvailable,
    registrationAvailable,
  };
}
