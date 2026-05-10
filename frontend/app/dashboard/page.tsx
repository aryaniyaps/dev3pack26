"use client";

import Link from "next/link";
import { Fragment, useCallback, useEffect, useState } from "react";
import { Connection, PublicKey } from "@solana/web3.js";
import { GroupInsightsPanel } from "@/components/dashboard/group-insights-panel";
import { useRubyLogout } from "@/hooks/use-ruby-logout";
import { useAuthStore } from "@/stores/auth-store";
import { useRubyEventsWs } from "@/hooks/use-ruby-events-ws";
import {
  buildContributeInstruction,
  buildJoinGroupInstruction,
  deriveGroupPda,
  explorerTxUrl,
  getBrowserSolanaSigner,
  signAndSendTransaction,
} from "@/lib/ruby-anchor";
import {
  buildTx,
  contribute,
  fetchGroupExplorer,
  fetchGroups,
  joinGroup,
  lamportsToSol,
  runTreasuryAgent,
  type Group,
  type TxPlan,
} from "@/lib/ruby-api";
import { useRouter } from "next/navigation";

const RPC = process.env.NEXT_PUBLIC_SOLANA_RPC_URL ?? "https://api.devnet.solana.com";

function DashboardInner() {
  const router = useRouter();
  const { walletAddress, email, principal } = useAuthStore();
  const logout = useRubyLogout();
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [agentBusy, setAgentBusy] = useState(false);
  const [txPlan, setTxPlan] = useState<TxPlan | null>(null);
  const [joinGroupId, setJoinGroupId] = useState("");
  const [joinInvite, setJoinInvite] = useState("");
  const [joinBusy, setJoinBusy] = useState(false);
  const [expandedGroupId, setExpandedGroupId] = useState<string | null>(null);

  const { events, connected } = useRubyEventsWs(null);

  const load = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const data = await fetchGroups();
      setGroups(Array.isArray(data) ? data : []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to load circles");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void Promise.resolve().then(() => load());
  }, [load]);

  const displayWallet = walletAddress || principal?.wallet_address || "";
  const label = displayWallet
    ? `${displayWallet.slice(0, 4)}…${displayWallet.slice(-4)}`
    : email || principal?.user_id?.slice(0, 8) || "Signed in";

  const handleLogout = async () => {
    await logout();
    router.replace("/");
  };

  const runAgent = async () => {
    setAgentBusy(true);
    setErr(null);
    try {
      await runTreasuryAgent({ dry_run: false });
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Agent run failed");
    } finally {
      setAgentBusy(false);
    }
  };

  const openExplorer = async (groupId: string) => {
    try {
      const links = await fetchGroupExplorer(groupId);
      const candidates = [links.group_pda_url, links.vault_url, links.token_url];
      const u = candidates.find((x) => x && !x.includes("/address/?"));
      if (u) window.open(u, "_blank", "noopener,noreferrer");
      else setErr("No on-chain addresses recorded for this circle yet.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Explorer link failed");
    }
  };

  const previewContributeTx = async (g: Group) => {
    setErr(null);
    try {
      const member = g.members?.find(
        (m) => displayWallet && m.wallet_address.toLowerCase() === displayWallet.toLowerCase(),
      );
      if (!member) {
        setErr("Join this circle first (your wallet is not a member yet).");
        return;
      }
      const plan = await buildTx("contribute", {
        group_id: g.id,
        member_id: member.id,
        wallet_address: displayWallet,
        amount: g.contribution_amt,
      });
      setTxPlan(plan);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "tx/build failed");
    }
  };

  const anchorContribute = async (g: Group) => {
    if (!displayWallet) {
      setErr("Wallet address required.");
      return;
    }
    const signer = getBrowserSolanaSigner();
    if (!signer) {
      setErr("Use Phantom on devnet to sign the contribute instruction.");
      return;
    }
    const sessionPk = new PublicKey(displayWallet);
    if (!sessionPk.equals(signer.publicKey)) {
      setErr("Phantom active account must match your session wallet.");
      return;
    }
    const member = g.members?.find(
      (m) => m.wallet_address.toLowerCase() === displayWallet.toLowerCase(),
    );
    if (!member) {
      setErr("Join this circle before contributing.");
      return;
    }
    const groupPda = deriveGroupPda(g.id);
    if (g.on_chain_pda && g.on_chain_pda !== groupPda.toBase58()) {
      setErr("Group PDA does not match program derivation — check NEXT_PUBLIC_RUBY_PROGRAM_ID.");
      return;
    }
    setErr(null);
    try {
      const onTime = g.cycle_deadline ? new Date(g.cycle_deadline) > new Date() : true;
      const connection = new Connection(RPC, "confirmed");
      const sig = await signAndSendTransaction({
        connection,
        feePayer: signer.publicKey,
        signTransaction: signer.signTransaction,
        instructions: [
          buildContributeInstruction(signer.publicKey, groupPda, BigInt(g.contribution_amt), onTime),
        ],
      });
      await contribute(g.id, {
        member_id: member.id,
        wallet_address: displayWallet,
        amount: g.contribution_amt,
        cycle_number: g.active_cycle || 1,
        tx_signature: sig,
      });
      await load();
      window.open(explorerTxUrl(sig), "_blank", "noopener,noreferrer");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Contribution failed";
      setErr(
        msg.includes("simulation") || msg.includes("custom program error")
          ? `${msg} — ensure this circle was created with on-chain create_group and you joined with join_group on the same devnet program.`
          : msg,
      );
    }
  };

  const handleJoin = async () => {
    if (!joinGroupId.trim() || !displayWallet) {
      setErr("Need a circle ID and a wallet on your session.");
      return;
    }
    setJoinBusy(true);
    setErr(null);
    const gid = joinGroupId.trim();
    try {
      const memberId = `m_${crypto.randomUUID().replace(/-/g, "").slice(0, 12)}`;
      await joinGroup(gid, {
        member_id: memberId,
        wallet_address: displayWallet,
        ...(joinInvite.trim() ? { invite_code: joinInvite.trim() } : {}),
      });
      try {
        const signer = getBrowserSolanaSigner();
        if (signer) {
          const sessionPk = new PublicKey(displayWallet);
          if (sessionPk.equals(signer.publicKey)) {
            const groupPda = deriveGroupPda(gid);
            await signAndSendTransaction({
              connection: new Connection(RPC, "confirmed"),
              feePayer: signer.publicKey,
              signTransaction: signer.signTransaction,
              instructions: [buildJoinGroupInstruction(signer.publicKey, groupPda)],
            });
          }
        }
      } catch (chainErr) {
        setErr(
          chainErr instanceof Error
            ? `Saved to app; on-chain join_group error: ${chainErr.message} (skip if already on-chain).`
            : "On-chain join error",
        );
      }
      setJoinGroupId("");
      setJoinInvite("");
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Join failed");
    } finally {
      setJoinBusy(false);
    }
  };

  return (
    <div className="min-h-screen bg-[var(--background)] text-[var(--foreground)]">
      <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8">
        <header className="flex flex-wrap items-center justify-between gap-4 border-b border-[var(--border-strong)] pb-6">
              <div>
            <h1 className="text-2xl font-semibold tracking-tight">Ruby · Circles</h1>
            <p className="text-sm text-[var(--muted)]">
              Live data from the API · WebSocket {connected ? "connected" : "reconnecting…"}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Link
              href="/create"
              className="rounded-lg bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-800"
            >
                  New circle
            </Link>
            <Link
              href="/activity"
              className="rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm hover:bg-slate-50"
            >
              Activity
            </Link>
            <span className="rounded-lg bg-slate-100 px-3 py-2 font-mono text-xs text-slate-700">{label}</span>
            <button
              type="button"
              onClick={() => void handleLogout()}
              className="rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm hover:bg-slate-50"
            >
              Log out
                </button>
              </div>
        </header>

        {err && (
          <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">{err}</div>
        )}

        <section className="grid gap-4 md:grid-cols-3">
          <div className="rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
            <div className="text-xs font-medium uppercase tracking-wide text-[var(--muted)]">Circles</div>
            <div className="mt-1 text-2xl font-semibold">{groups.length}</div>
                </div>
          <div className="rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
            <div className="text-xs font-medium uppercase tracking-wide text-[var(--muted)]">Stream events</div>
            <div className="mt-1 text-2xl font-semibold">{events.length}</div>
            <div className="mt-2 max-h-24 overflow-y-auto font-mono text-[10px] text-slate-600">
              {events.slice(0, 5).map((e, i) => (
                <div key={i}>
                  {e.type}{" "}
                  {typeof e.payload?.group_id === "string" ? `· ${e.payload.group_id}` : ""}
                </div>
              ))}
                </div>
              </div>
          <div className="flex flex-col justify-between rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
                <div>
              <div className="text-xs font-medium uppercase tracking-wide text-[var(--muted)]">Treasury agent</div>
              <p className="mt-1 text-sm text-[var(--muted)]">Runs the Go scheduler against all circles (demo).</p>
            </div>
            <button
              type="button"
              disabled={agentBusy}
              onClick={() => void runAgent()}
              className="mt-3 rounded-lg bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              {agentBusy ? "Running…" : "Run agent now"}
            </button>
          </div>
        </section>

        <section className="rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-4 shadow-sm">
          <h2 className="text-sm font-semibold">Join a circle</h2>
          <p className="mt-1 text-xs text-[var(--muted)]">Paste an invite or circle ID from your organiser.</p>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
            <input
              className="w-full rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm"
              placeholder="Circle ID (e.g. g_abc123)"
              value={joinGroupId}
              onChange={(e) => setJoinGroupId(e.target.value)}
            />
            <input
              className="w-full rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm"
              placeholder="Invite code (optional)"
              value={joinInvite}
              onChange={(e) => setJoinInvite(e.target.value)}
            />
            <button
              type="button"
              disabled={joinBusy}
              onClick={() => void handleJoin()}
              className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
            >
              {joinBusy ? "Joining…" : "Join"}
            </button>
                </div>
        </section>

        <section className="overflow-hidden rounded-xl border border-[var(--border-strong)] bg-[var(--card)] shadow-sm">
          <div className="flex items-center justify-between border-b border-[var(--border-strong)] px-4 py-3">
            <h2 className="text-sm font-semibold">Your circles (API)</h2>
            <button
              type="button"
              onClick={() => void load()}
              className="text-sm text-indigo-600 hover:underline"
            >
              Refresh
            </button>
              </div>
          {loading ? (
            <div className="p-8 text-center text-sm text-[var(--muted)]">Loading…</div>
          ) : groups.length === 0 ? (
            <div className="p-8 text-center text-sm text-[var(--muted)]">
              No circles yet —{" "}
              <Link href="/create" className="text-indigo-600 hover:underline">
                create one
              </Link>
              .
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-[var(--muted)]">
                  <tr>
                    <th className="px-4 py-2">Name</th>
                    <th className="px-4 py-2">ID</th>
                    <th className="px-4 py-2">Members</th>
                    <th className="px-4 py-2">Vault (SOL)</th>
                    <th className="px-4 py-2">Cycle</th>
                    <th className="px-4 py-2">Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                  {groups.map((g) => {
                    const myMember = g.members?.find(
                      (m) => displayWallet && m.wallet_address.toLowerCase() === displayWallet.toLowerCase(),
                    );
                    return (
                      <Fragment key={g.id}>
                        <tr className="border-t border-[var(--border-strong)]">
                          <td className="px-4 py-3 font-medium">{g.name}</td>
                          <td className="px-4 py-3 font-mono text-xs">{g.id}</td>
                          <td className="px-4 py-3">{g.members?.length ?? 0}</td>
                          <td className="px-4 py-3">{lamportsToSol(g.vault_balance)}</td>
                          <td className="px-4 py-3">
                            {g.active_cycle}
                            {g.cycle_deadline
                              ? ` · due ${new Date(g.cycle_deadline).toLocaleDateString()}`
                              : ""}
                            </td>
                          <td className="px-4 py-3">
                            <div className="flex flex-wrap gap-2">
                              <button
                                type="button"
                                className="rounded border border-[var(--border-strong)] px-2 py-1 text-xs hover:bg-slate-50"
                                onClick={() =>
                                  setExpandedGroupId((id) => (id === g.id ? null : g.id))
                                }
                              >
                                {expandedGroupId === g.id ? "Hide" : "Insights"}
                              </button>
                              <button
                                type="button"
                                className="rounded border border-[var(--border-strong)] px-2 py-1 text-xs hover:bg-slate-50"
                                onClick={() => void openExplorer(g.id)}
                              >
                                Explorer
                              </button>
                              <button
                                type="button"
                                className="rounded border border-[var(--border-strong)] px-2 py-1 text-xs hover:bg-slate-50"
                                onClick={() => void previewContributeTx(g)}
                              >
                                Tx plan
                              </button>
                              <button
                                type="button"
                                className="rounded bg-emerald-700 px-2 py-1 text-xs text-white hover:bg-emerald-600"
                                onClick={() => void anchorContribute(g)}
                              >
                                Anchor contribute
                              </button>
                              </div>
                            </td>
                        </tr>
                        {expandedGroupId === g.id && (
                          <tr className="border-t border-[var(--border-strong)]">
                            <td colSpan={6} className="p-0">
                              <GroupInsightsPanel
                                key={g.id}
                                group={g}
                                walletAddress={displayWallet}
                                memberId={myMember?.id ?? null}
                              />
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                      </tbody>
                    </table>
                  </div>
          )}
        </section>

        {txPlan && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog">
            <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-xl bg-white p-6 shadow-xl">
              <h3 className="text-lg font-semibold">Anchor tx plan (backend)</h3>
              <p className="mt-2 text-xs text-[var(--muted)]">
                Instruction <strong>{txPlan.instruction_name}</strong> on {txPlan.network} · IDL loaded:{" "}
                {String(txPlan.has_idl)}
              </p>
              <pre className="mt-4 max-h-64 overflow-auto rounded-lg bg-slate-900 p-3 text-xs text-slate-100">
                {JSON.stringify(txPlan, null, 2)}
              </pre>
              <button
                type="button"
                className="mt-4 w-full rounded-lg bg-slate-900 py-2 text-sm text-white"
                onClick={() => setTxPlan(null)}
              >
                Close
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default function RubyDashboardPage() {
  return <DashboardInner />;
}
