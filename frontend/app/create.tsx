"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Connection, PublicKey } from "@solana/web3.js";
import { useAuthStore } from "@/stores/auth-store";
import {
  buildCreateGroupInstruction,
  buildJoinGroupInstruction,
  deriveGroupPda,
  explorerTxUrl,
  getBrowserSolanaSigner,
  signAndSendTransaction,
} from "@/lib/ruby-anchor";
import { createGroup, joinGroup, solToLamports } from "@/lib/ruby-api";

const RPC = process.env.NEXT_PUBLIC_SOLANA_RPC_URL ?? "https://api.devnet.solana.com";

function CreateInner() {
  const router = useRouter();
  const { walletAddress, principal } = useAuthStore();
  const displayWallet = walletAddress || principal?.wallet_address || "";

  const [name, setName] = useState("");
  const [contributionSol, setContributionSol] = useState("0.05");
  const [maxMembers, setMaxMembers] = useState(10);
  const [busy, setBusy] = useState(false);
  const [log, setLog] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!displayWallet) {
      setLog("You need a wallet on this session (Phantom or Privy embedded wallet).");
      return;
    }
    const sol = Number(contributionSol);
    if (!name.trim() || !Number.isFinite(sol) || sol <= 0) {
      setLog("Name and a positive SOL contribution are required.");
      return;
    }

    setBusy(true);
    setLog(null);
    try {
      const signer = getBrowserSolanaSigner();
      if (!signer) {
        setLog("Use Phantom (browser extension) on devnet to sign create_group + join_group.");
        return;
      }
      const pk = new PublicKey(displayWallet);
      if (!pk.equals(signer.publicKey)) {
        setLog("Session wallet must match Phantom’s active account.");
        return;
      }

      const groupId = `g_${crypto.randomUUID().replace(/-/g, "").slice(0, 12)}`;
      const lamports = solToLamports(sol);
      const groupPda = deriveGroupPda(groupId);
      const connection = new Connection(RPC, "confirmed");

      const chainSig = await signAndSendTransaction({
        connection,
        feePayer: signer.publicKey,
        signTransaction: signer.signTransaction,
        instructions: [
          buildCreateGroupInstruction({
            creator: signer.publicKey,
            groupId,
            contributionLamports: BigInt(lamports),
            maxMembers,
          }),
          buildJoinGroupInstruction(signer.publicKey, groupPda),
        ],
      });

      await createGroup({
        id: groupId,
        name: name.trim(),
        creator_wallet: displayWallet,
        contribution_amt: lamports,
        max_members: maxMembers,
        on_chain_pda: groupPda.toBase58(),
      });

      const memberId = `m_${crypto.randomUUID().replace(/-/g, "").slice(0, 12)}`;
      await joinGroup(groupId, {
        member_id: memberId,
        wallet_address: displayWallet,
      });

      setLog(
        `On-chain circle initialized. Tx: ${chainSig}\nExplorer: ${explorerTxUrl(chainSig)}\nAPI member: ${memberId}`,
      );
      setTimeout(() => router.replace("/dashboard"), 1500);
    } catch (err) {
      setLog(err instanceof Error ? err.message : "Create failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen bg-[var(--background)] px-4 py-10 text-[var(--foreground)]">
      <div className="mx-auto max-w-lg">
        <Link href="/dashboard" className="text-sm text-indigo-600 hover:underline">
          ← Back to dashboard
        </Link>
        <h1 className="mt-6 text-2xl font-semibold">Create a circle</h1>
        <p className="mt-2 text-sm text-[var(--muted)]">
          Signs a real devnet transaction: <code className="rounded bg-slate-100 px-1">create_group</code> +{" "}
          <code className="rounded bg-slate-100 px-1">join_group</code>, then syncs Postgres via the API.
        </p>

        {!displayWallet && (
          <p className="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
            No wallet on this session. You need a devnet-funded Phantom wallet: the create flow signs real{" "}
            <code className="rounded bg-amber-100 px-1">create_group</code> +{" "}
            <code className="rounded bg-amber-100 px-1">join_group</code> instructions before saving to the API.
          </p>
        )}

        <form onSubmit={(e) => void submit(e)} className="mt-8 space-y-4 rounded-xl border border-[var(--border-strong)] bg-[var(--card)] p-6 shadow-sm">
          <div>
            <label className="text-xs font-medium uppercase text-[var(--muted)]">Name</label>
            <input
              className="mt-1 w-full rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Mama Chama Circle"
              required
            />
          </div>
          <div>
            <label className="text-xs font-medium uppercase text-[var(--muted)]">
              Contribution (SOL, devnet)
            </label>
            <input
              className="mt-1 w-full rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm"
              type="number"
              step="any"
              min="0.000000001"
              value={contributionSol}
              onChange={(e) => setContributionSol(e.target.value)}
            />
          </div>
          <div>
            <label className="text-xs font-medium uppercase text-[var(--muted)]">Max members</label>
            <input
              className="mt-1 w-full rounded-lg border border-[var(--border-strong)] px-3 py-2 text-sm"
              type="number"
              min={2}
              max={200}
              value={maxMembers}
              onChange={(e) => setMaxMembers(Number(e.target.value))}
            />
          </div>
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-lg bg-slate-900 py-2.5 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create & join"}
          </button>
        </form>

        {log && (
          <pre className="mt-6 whitespace-pre-wrap rounded-lg border border-[var(--border-strong)] bg-slate-50 p-4 text-xs">
            {log}
          </pre>
        )}
      </div>
    </div>
  );
}

export default function CreatePage() {
  return <CreateInner />;
}
