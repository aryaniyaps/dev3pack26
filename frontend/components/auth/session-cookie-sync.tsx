"use client";

import { useEffect } from "react";
import { useAuthStore } from "@/stores/auth-store";
import { clearRubySessionCookie, setRubySessionCookie } from "@/lib/session-cookie";

/**
 * Mirrors persisted Ruby session into a first-party cookie so Edge middleware
 * can align with Privy's cookie-based SSR guidance.
 */
export function SessionCookieSync() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const hasHydrated = useAuthStore((s) => s.hasHydrated);

  useEffect(() => {
    if (!hasHydrated) {
      return;
    }
    if (isAuthenticated) {
      setRubySessionCookie();
    } else {
      clearRubySessionCookie();
    }
  }, [hasHydrated, isAuthenticated]);

  return null;
}
