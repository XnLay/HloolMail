type OAuthErrorState = {
  code: string;
  provider: string;
};

export function oauthErrorFromLocation(): OAuthErrorState | null {
  if (typeof window === 'undefined') return null;
  const fromSearch = oauthErrorFromParams(new URLSearchParams(window.location.search));
  if (fromSearch) return fromSearch;
  const queryStart = window.location.hash.indexOf('?');
  if (queryStart < 0) return null;
  return oauthErrorFromParams(new URLSearchParams(window.location.hash.slice(queryStart + 1)));
}

function oauthErrorFromParams(params: URLSearchParams): OAuthErrorState | null {
  const code = params.get('oauth_error');
  if (!code) return null;
  return {
    code,
    provider: params.get('oauth_provider') || '',
  };
}

export function oauthProviderDisplayName(provider: string) {
  if (provider === 'github') return 'GitHub';
  if (provider === 'linuxdo') return 'Linux.do';
  return provider;
}
