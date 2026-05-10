/**
 * Lightweight cookie so Next.js middleware can gate routes while the Ruby JWT
 * lives in localStorage (zustand persist). API calls still require the Bearer token.
 * Must stay in sync with {@link SessionCookieSync}.
 */
export const RUBY_SESSION_COOKIE = "ruby_authed";

export function setRubySessionCookie(): void {
  if (typeof document === "undefined") {
    return;
  }
  const secure = typeof window !== "undefined" && window.location.protocol === "https:";
  document.cookie = `${RUBY_SESSION_COOKIE}=1; Path=/; Max-Age=${60 * 60 * 24 * 7}; SameSite=Lax${secure ? "; Secure" : ""}`;
}

export function clearRubySessionCookie(): void {
  if (typeof document === "undefined") {
    return;
  }
  document.cookie = `${RUBY_SESSION_COOKIE}=; Path=/; Max-Age=0`;
}
