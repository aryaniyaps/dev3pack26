"use client";

import { Suspense, useEffect, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { usePrivy } from "@privy-io/react-auth";
import { AuthLoading } from "@/components/auth/auth-loading";
import { AUTH_REDIRECT_PATH, LOGIN_PATH } from "@/lib/auth-paths";

/**
 * Privy session refresh landing (https://docs.privy.io/recipes/react/cookies).
 * When middleware sees `privy-session` but no `privy-token`, it redirects here so
 * the client can refresh tokens before continuing to the original URL.
 */
function PrivyRefreshInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { ready, authenticated, getAccessToken } = usePrivy();
  const ran = useRef(false);

  useEffect(() => {
    if (!ready || ran.current) {
      return;
    }
    ran.current = true;

    const redirectToRaw = searchParams.get("redirect_to") ?? AUTH_REDIRECT_PATH;
    const redirectTo =
      redirectToRaw.startsWith("/") && !redirectToRaw.startsWith("//") ? redirectToRaw : AUTH_REDIRECT_PATH;

    void (async () => {
      if (!authenticated) {
        router.replace(LOGIN_PATH);
        return;
      }
      const t = await getAccessToken();
      if (t) {
        router.replace(redirectTo);
      } else {
        router.replace(LOGIN_PATH);
      }
    })();
  }, [authenticated, getAccessToken, ready, router, searchParams]);

  return <AuthLoading label="Refreshing session…" />;
}

export default function PrivyRefreshPage() {
  return (
    <Suspense fallback={<AuthLoading label="Loading…" />}>
      <PrivyRefreshInner />
    </Suspense>
  );
}
