"use client";

import { usePrivy } from "@privy-io/react-auth";
import { useCallback } from "react";
import { useAuthStore } from "@/stores/auth-store";

function privyEnabled(): boolean {
  return Boolean(process.env.NEXT_PUBLIC_PRIVY_APP_ID?.trim());
}

/**
 * Clears the Ruby session (API + persisted store) then signs out of Privy when the app
 * is configured for Privy. Keeps browser state consistent so the login page does not
 * show “Privy authenticated” with no Ruby token after logout.
 */
export function useRubyLogout() {
  const storeLogout = useAuthStore((s) => s.logout);
  const { authenticated, logout: privyLogout } = usePrivy();

  return useCallback(async () => {
    await storeLogout();
    if (!privyEnabled() || !authenticated) {
      return;
    }
    try {
      await privyLogout();
    } catch {
      // Session may already be invalid; Ruby local state is already cleared.
    }
  }, [authenticated, privyLogout, storeLogout]);
}
