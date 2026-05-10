"use client";

import { Button } from "@/components/ui/button";
import { useRubyLogout } from "@/hooks/use-ruby-logout";
import { useAuthStore } from "@/stores/auth-store";
import { formatWalletAddress } from "@/lib/utils";
import { LogOut, User } from "lucide-react";

interface HeaderProps {
  onLoginClick?: () => void;
}

export function Header({ onLoginClick }: HeaderProps) {
  const { isAuthenticated, walletAddress, email } = useAuthStore();
  const logout = useRubyLogout();

  return (
    <header className="sticky top-0 z-20 border-b border-slate-200/70 bg-white/90 backdrop-blur-xl">
      <div className="container mx-auto px-4 py-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="h-9 w-9 rounded-2xl bg-slate-950/5 ring-1 ring-slate-200/80" />
            <div>
              <p className="text-sm font-semibold tracking-tight text-slate-950">Project Ruby</p>
              <p className="text-xs text-slate-500">Autonomous savings on Solana</p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <span className="rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">
              Devnet
            </span>
            {isAuthenticated ? (
              <div className="flex items-center gap-3 rounded-full border border-slate-200 bg-slate-50 px-4 py-2">
                <div className="flex items-center gap-2 text-sm text-slate-700">
                  <User className="h-4 w-4" />
                  {walletAddress ? (
                    <span>{formatWalletAddress(walletAddress)}</span>
                  ) : email ? (
                    <span>{email}</span>
                  ) : null}
                </div>
                <Button variant="ghost" size="sm" onClick={logout} className="text-slate-700 hover:text-slate-950">
                  <LogOut className="h-4 w-4 mr-2" />
                  Logout
                </Button>
              </div>
            ) : (
              <Button onClick={onLoginClick} variant="default" size="sm" className="rounded-full">
                Sign In
              </Button>
            )}
          </div>
        </div>
      </div>
    </header>
  );
}
