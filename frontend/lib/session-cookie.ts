/**
 * First-party hint for Next proxy (`ruby_authed`). The real session is the HttpOnly
 * `ruby_session` on the API host and/or the Ruby session JWT in the client (Bearer).
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
