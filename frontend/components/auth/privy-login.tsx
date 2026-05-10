"use client";

import { useCallback, useEffect, useRef } from "react";
import { getIdentityToken, useIdentityToken, usePrivy } from "@privy-io/react-auth";
import { useAuthStore } from "@/stores/auth-store";

interface PrivyLoginProps {
  onSuccess?: () => void;
  onError?: (message: string) => void;
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

/**
 * Keeps Privy (browser session) and Ruby (HttpOnly session cookie + persisted principal) in sync.
 * If Privy already has `authenticated: true` but Ruby has no session, calling `login()`
 * throws ("already logged in… use link"). We must exchange a Privy JWT
 * (`usePrivy().getAccessToken()` or identity token) for a Ruby session.
 *
 * Important: `useUser().refreshUser()` maps to Privy `GET /api/v1/users/me` (strictly rate-limited). We avoid it here
 * and rely on `getAccessToken` / identity token so devtools / React Strict Mode do not trigger 429 bursts.
 */
export function PrivyLogin({ onSuccess, onError }: PrivyLoginProps) {
  const { authenticated, login, ready, user, getAccessToken } = usePrivy();
  const { identityToken } = useIdentityToken();
  const identityTokenRef = useRef<string | null>(null);
  const rubyAuthed = useAuthStore((s) => s.isAuthenticated);
  const hasHydrated = useAuthStore((s) => s.hasHydrated);
  const { verifyPrivyToken, isLoading } = useAuthStore();
  const lastOkIdentityRef = useRef<string | null>(null);
  const inFlightRef = useRef(false);
  const onSuccessRef = useRef(onSuccess);
  const onErrorRef = useRef(onError);

  useEffect(() => {
    identityTokenRef.current = identityToken ?? null;
  }, [identityToken]);

  useEffect(() => {
    onSuccessRef.current = onSuccess;
  }, [onSuccess]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const emailAddr = user?.email?.address ?? null;

  const syncRubyFromPrivy = useCallback(async () => {
    if (!hasHydrated) {
      return;
    }
    if (!ready || !authenticated) {
      return;
    }
    if (useAuthStore.getState().isAuthenticated) {
      return;
    }
    if (inFlightRef.current) {
      return;
    }

    const readTokensWithoutRefresh = async (): Promise<string | null> => {
      // Must use `usePrivy().getAccessToken()` — the Privy ES256 JWT verifiable with app JWKS.
      // Do NOT import `getAccessToken` from `@privy-io/react-auth`: the package re-exports
      // `getCustomerAccessToken` under that name; it is not the Privy app access JWT.
      let id = (await getAccessToken()) ?? null;
      if (!id) {
        id = identityTokenRef.current;
      }
      if (!id) {
        id = (await getIdentityToken()) ?? null;
      }
      return id;
    };

    let id = await readTokensWithoutRefresh();
    if (!id) {
      await sleep(450);
      id = await readTokensWithoutRefresh();
    }
    if (!id) {
      await sleep(900);
      id = await readTokensWithoutRefresh();
    }

    if (!id) {
      onErrorRef.current?.(
        "Ruby could not read a Privy JWT yet (Privy may be rate-limiting after many refreshes). Wait 30–60 seconds, click \"Verify & continue\" once, or reload the page. Confirm the backend has PRIVY_APP_ID and PRIVY_JWKS_URL for this app.",
      );
      return;
    }

    if (lastOkIdentityRef.current === id) {
      return;
    }

    inFlightRef.current = true;
    try {
      await verifyPrivyToken(id, emailAddr);
      lastOkIdentityRef.current = id;
      onSuccessRef.current?.();
    } catch (error) {
      lastOkIdentityRef.current = null;
      onErrorRef.current?.(error instanceof Error ? error.message : "Privy verification failed");
    } finally {
      inFlightRef.current = false;
    }
  }, [authenticated, emailAddr, getAccessToken, hasHydrated, ready, verifyPrivyToken]);

  useEffect(() => {
    if (!rubyAuthed) {
      lastOkIdentityRef.current = null;
    }
  }, [rubyAuthed]);

  useEffect(() => {
    const t = window.setTimeout(() => {
      void syncRubyFromPrivy();
    }, 350);
    return () => window.clearTimeout(t);
  }, [syncRubyFromPrivy, rubyAuthed]);

  const handleClick = () => {
    if (!ready || isLoading) {
      return;
    }
    if (authenticated) {
      if (!useAuthStore.getState().isAuthenticated) {
        void syncRubyFromPrivy();
      }
      return;
    }
    login({ loginMethods: ["email"] });
  };

  const needsRubySync = authenticated && !rubyAuthed;
  const label = !authenticated
    ? "Continue with email"
    : needsRubySync
      ? "Verify & continue"
      : "Continue with email";

  return (
    <button className="ruby-btn ruby-btn-primary" onClick={handleClick} disabled={!ready || isLoading} type="button">
      {isLoading ? (
        <>
          <div className="loading-spinner" />
          Verifying...
        </>
      ) : (
        <>
          <i className="ti ti-mail" style={{ fontSize: 13 }} />
          {label}
        </>
      )}
    </button>
  );
}
