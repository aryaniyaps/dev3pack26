/** Post-login and default protected landing */
export const AUTH_REDIRECT_PATH = "/dashboard";

/** Public login / landing */
export const LOGIN_PATH = "/";

/** Privy OAuth query params — middleware must not strip these (Privy docs). */
export const PRIVY_OAUTH_QUERY_KEYS = ["privy_oauth_code", "privy_oauth_state", "privy_oauth_provider"] as const;

const PROTECTED_PREFIXES = ["/dashboard", "/activity", "/create"] as const;

/** App routes that require an auth signal (Privy cookies or Ruby session cookie). */
export function isProtectedPath(pathname: string): boolean {
  return PROTECTED_PREFIXES.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

export function hasPrivyOAuthParams(searchParams: URLSearchParams): boolean {
  return PRIVY_OAUTH_QUERY_KEYS.some((k) => searchParams.has(k));
}
