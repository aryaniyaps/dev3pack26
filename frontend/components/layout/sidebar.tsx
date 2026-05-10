"use client";

import { Button } from "@/components/ui/button";
import { useRubyLogout } from "@/hooks/use-ruby-logout";
import { Home, Users, TrendingUp, Bell, FileText, Link as LinkIcon, Wallet, Settings, LogOut } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

interface SidebarProps {
  className?: string;
}

export function Sidebar({ className }: SidebarProps) {
  const logout = useRubyLogout();
  const pathname = usePathname();

  const menuItems = [
    { href: "/dashboard", label: "Overview", icon: Home },
    { href: "/groups", label: "My circles", icon: Users },
    { href: "/yield", label: "Yield history", icon: TrendingUp },
    { href: "/activity", label: "Activity", icon: Bell },
    { href: "/loans", label: "Loan requests", icon: FileText },
    { href: "/blinks", label: "Blinks", icon: LinkIcon },
    { href: "/wallet", label: "My wallet", icon: Wallet },
    { href: "/settings", label: "Settings", icon: Settings },
  ];

  return (
    <aside className={`w-full min-w-[210px] border-r border-slate-200 bg-white lg:w-[210px] lg:border-b-0 ${className}`}>
      <div className="p-4 border-b border-slate-200 flex items-center gap-2.5">
        <div className="h-9 w-9 rounded-2xl bg-slate-950 flex items-center justify-center text-white font-bold">R</div>
        <span className="text-sm font-semibold tracking-tight text-slate-950">Ruby</span>
      </div>

      <div className="p-3">
        <div className="text-[9px] font-bold uppercase tracking-[0.14em] text-slate-500 mb-1.5">Main</div>
        {menuItems.slice(0, 4).map((item) => {
          const Icon = item.icon;
          const isActive = pathname.startsWith(item.href);
          return (
            <Link key={item.href} href={item.href} className={`flex items-center gap-2.5 py-2 px-3 text-xs font-medium text-slate-600 hover:bg-slate-50 rounded-lg mb-0.5 ${isActive ? 'bg-slate-50 text-slate-950 font-semibold' : ''}`}>
              <Icon className="h-3.5 w-3.5" />
              {item.label}
            </Link>
          );
        })}
        <div className="text-[9px] font-bold uppercase tracking-[0.14em] text-slate-500 mb-1.5 mt-3">Governance</div>
        {menuItems.slice(4, 6).map((item) => {
          const Icon = item.icon;
          const isActive = pathname.startsWith(item.href);
          return (
            <Link key={item.href} href={item.href} className={`flex items-center gap-2.5 py-2 px-3 text-xs font-medium text-slate-600 hover:bg-slate-50 rounded-lg mb-0.5 ${isActive ? 'bg-slate-50 text-slate-950 font-semibold' : ''}`}>
              <Icon className="h-3.5 w-3.5" />
              {item.label}
            </Link>
          );
        })}
        <div className="text-[9px] font-bold uppercase tracking-[0.14em] text-slate-500 mb-1.5 mt-3">Account</div>
        {menuItems.slice(6).map((item) => {
          const Icon = item.icon;
          const isActive = pathname.startsWith(item.href);
          return (
            <Link key={item.href} href={item.href} className={`flex items-center gap-2.5 py-2 px-3 text-xs font-medium text-slate-600 hover:bg-slate-50 rounded-lg mb-0.5 ${isActive ? 'bg-slate-50 text-slate-950 font-semibold' : ''}`}>
              <Icon className="h-3.5 w-3.5" />
              {item.label}
            </Link>
          );
        })}
      </div>

      <div className="p-3 mt-auto">
        <div className="flex items-center gap-2.5 p-3 rounded-full border border-slate-200 bg-slate-50">
          <div className="h-8 w-8 rounded-full bg-slate-950 flex items-center justify-center text-white font-bold text-xs">AK</div>
          <div>
            <div className="text-xs font-semibold text-slate-950">Amara Kioni</div>
            <div className="text-[10px] text-slate-500">Devnet · 6Xp7…3fKL</div>
          </div>
        </div>
        <Button variant="ghost" onClick={logout} className="w-full justify-start text-slate-700 hover:text-slate-950 mt-2 text-xs">
          <LogOut className="h-3 w-3 mr-2" />
          Logout
        </Button>
      </div>
    </aside>
  );
}
