"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRubyEventsWs } from "@/hooks/use-ruby-events-ws";
import { fetchChainEvents, type ChainEvent } from "@/lib/ruby-api";

function ActivityInner() {
  const { events, connected, error: wsError } = useRubyEventsWs(null);
  const [rows, setRows] = useState<ChainEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [restErr, setRestErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await fetchChainEvents(80);
        if (!cancelled) setRows(data);
      } catch (e) {
        if (!cancelled) setRestErr(e instanceof Error ? e.message : "Failed to load chain events");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="min-h-screen bg-[var(--background)] px-4 py-10 text-[var(--foreground)]">
      <div className="mx-auto max-w-4xl">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold">Activity</h1>
            <p className="text-sm text-[var(--muted)]">
              WebSocket stream + Helius-backed <code className="rounded bg-slate-100 px-1">chain_events</code>{" "}
              table (
              {connected ? "live" : "connecting…"})
            </p>
          </div>
          <Link href="/dashboard" className="text-sm text-indigo-600 hover:underline">
            ← Dashboard
          </Link>
        </div>

        {wsError && (
          <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
            WebSocket: {wsError}
          </div>
        )}
        {restErr && (
          <div className="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800">
            REST: {restErr}
          </div>
        )}

        <div className="mt-8 grid gap-6 lg:grid-cols-2">
          <section className="rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
            <h2 className="text-sm font-semibold">Live stream</h2>
            <ul className="mt-3 max-h-[480px] space-y-2 overflow-y-auto font-mono text-xs">
              {events.map((e, i) => (
                <li key={i} className="rounded border border-slate-100 bg-slate-50 p-2">
                  <div className="font-semibold text-slate-800">{e.type}</div>
                  <div className="text-slate-500">{e.timestamp}</div>
                  <pre className="mt-1 overflow-x-auto text-[10px] text-slate-600">
                    {JSON.stringify(e.payload)}
                  </pre>
                </li>
              ))}
              {events.length === 0 && (
                <li className="text-[var(--muted)]">Waiting for events (contributions, agent, Helius)…</li>
              )}
            </ul>
          </section>

          <section className="rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
            <h2 className="text-sm font-semibold">Persisted chain events</h2>
            {loading ? (
              <p className="mt-4 text-sm text-[var(--muted)]">Loading…</p>
            ) : (
              <ul className="mt-3 max-h-[480px] space-y-2 overflow-y-auto text-xs">
                {rows.map((r) => (
                  <li key={r.id} className="rounded border border-slate-100 p-2">
                    <div className="font-medium">{r.event_type}</div>
                    <div className="text-[var(--muted)]">
                      {r.group_id ? `group ${r.group_id} · ` : ""}
                      {new Date(r.created_at).toLocaleString()}
                    </div>
                    {r.signature ? (
                      <a
                        className="mt-1 inline-block text-indigo-600 hover:underline"
                        href={`https://explorer.solana.com/tx/${r.signature}?cluster=devnet`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {r.signature.slice(0, 12)}…
                      </a>
                    ) : null}
                  </li>
                ))}
                {rows.length === 0 && (
                  <li className="text-[var(--muted)]">
                    No rows yet — POST a sample payload to{" "}
                    <code className="rounded bg-slate-100 px-1">/api/v1/webhooks/helius</code> or wire Helius.
                  </li>
                )}
              </ul>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

export default function ActivityPage() {
  return <ActivityInner />;
}
