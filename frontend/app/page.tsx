"use client";

import { useLayoutEffect } from "react";
import { useRouter } from "next/navigation";
import { LoginForm } from "@/components/auth/login-form";
import { AuthLoading } from "@/components/auth/auth-loading";
import { useAuthStore } from "@/stores/auth-store";
import { AUTH_REDIRECT_PATH } from "@/lib/auth-paths";

export default function HomePage() {
  const router = useRouter();
  const { hasHydrated, token } = useAuthStore();

  useLayoutEffect(() => {
    if (!hasHydrated || !token) {
      return;
    }
    let next = AUTH_REDIRECT_PATH;
    if (typeof window !== "undefined") {
      const raw = new URLSearchParams(window.location.search).get("returnUrl");
      if (raw && raw.startsWith("/") && !raw.startsWith("//")) {
        next = raw;
      }
    }
    router.replace(next);
  }, [hasHydrated, token, router]);

  if (!hasHydrated) {
    return <AuthLoading />;
  }

  if (token) {
    return <AuthLoading label="Opening dashboard…" />;
  }

  return (
    <>
      <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@tabler/icons-webfont@latest/tabler-icons.min.css" />
      <div
        style={{
          minHeight: "100vh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          padding: 24,
          background: "#fafaf9",
        }}
      >
        <LoginForm />
      </div>
    </>
  );
}
