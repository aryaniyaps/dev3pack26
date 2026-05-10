import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";
import { hasPrivyOAuthParams, isProtectedPath, LOGIN_PATH } from "@/lib/auth-paths";
import { RUBY_SESSION_COOKIE } from "@/lib/session-cookie";

/**
 * Edge proxy for protected routes (Next.js 16+ `proxy.ts`; same role as legacy `middleware.ts`).
 * Privy cookie semantics: https://docs.privy.io/recipes/react/cookies
 *
 * - `privy-token`: valid Privy access JWT for this request
 * - `privy-session`: client may refresh → `/refresh`
 * - `ruby_authed`: Ruby session hint (see SessionCookieSync); API still validates Bearer JWT
 */
export function proxy(req: NextRequest) {
  const { pathname, searchParams } = req.nextUrl;

  if (hasPrivyOAuthParams(searchParams)) {
    return NextResponse.next();
  }

  if (pathname === "/refresh" || pathname.startsWith("/refresh/")) {
    return NextResponse.next();
  }

  if (!isProtectedPath(pathname)) {
    return NextResponse.next();
  }

  const privyToken = req.cookies.get("privy-token")?.value;
  const privySession = req.cookies.get("privy-session")?.value;
  const rubyAuthed = req.cookies.get(RUBY_SESSION_COOKIE)?.value === "1";

  if (privyToken || rubyAuthed) {
    return NextResponse.next();
  }

  if (privySession) {
    const url = req.nextUrl.clone();
    url.pathname = "/refresh";
    url.search = "";
    url.searchParams.set("redirect_to", `${pathname}${req.nextUrl.search}`);
    return NextResponse.redirect(url);
  }

  const login = req.nextUrl.clone();
  login.pathname = LOGIN_PATH;
  login.search = "";
  login.searchParams.set("returnUrl", `${pathname}${req.nextUrl.search}`);
  return NextResponse.redirect(login);
}

export const config = {
  matcher: [
    "/((?!_next/static|_next/image|favicon.ico|icon.svg|.*\\.(?:svg|png|jpg|jpeg|gif|webp)$).*)",
  ],
};
