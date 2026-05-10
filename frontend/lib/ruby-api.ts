const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ??
  `${process.env.NEXT_PUBLIC_BACKEND_URL ?? "http://localhost:8080"}/api/v1`;

function apiInit(extra: RequestInit = {}): RequestInit {
  const extraHeaders =
    extra.headers && typeof extra.headers === "object" && !Array.isArray(extra.headers)
      ? (extra.headers as Record<string, string>)
      : {};
  return {
    credentials: "include",
    ...extra,
    headers: {
      "Content-Type": "application/json",
      ...extraHeaders,
    },
  };
}

export type Group = {
  id: string;
  name: string;
  creator_wallet: string;
  contribution_amt: number;
  cycle_count: number;
  max_members: number;
  vault_balance: number;
  swig_vault_addr: string;
  on_chain_pda: string;
  active_cycle: number;
  cycle_deadline?: string;
  payout_ready: boolean;
  created_at: string;
  members?: Member[];
};

export type Member = {
  id: string;
  group_id: string;
  wallet_address: string;
  credit_score: number;
  total_contributed: number;
  joined_at: string;
};

export type ChainEvent = {
  id: number;
  group_id: string;
  event_type: string;
  signature: string;
  raw_payload: string;
  created_at: string;
};

export type TxPlan = {
  network: string;
  program_id: string;
  instruction_name: string;
  accounts: Record<string, unknown>;
  args: Record<string, unknown>;
  has_idl: boolean;
};

export type WsEvent = {
  type: string;
  timestamp: string;
  payload: Record<string, unknown>;
};

async function parseError(res: Response): Promise<string> {
  let message = `Request failed (${res.status})`;
  try {
    const body = await res.json();
    if (typeof body?.error === "string") message = body.error;
  } catch {
    /* ignore */
  }
  return message;
}

export async function fetchGroups(): Promise<Group[]> {
  const res = await fetch(`${API_BASE}/groups`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  const data: unknown = await res.json();
  return Array.isArray(data) ? (data as Group[]) : [];
}

export async function fetchChainEvents(limit = 50): Promise<ChainEvent[]> {
  const res = await fetch(`${API_BASE}/chain-events?limit=${limit}`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<ChainEvent[]>;
}

export async function fetchOnchainConfig(): Promise<Record<string, unknown>> {
  const res = await fetch(`${API_BASE}/onchain/config`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<Record<string, unknown>>;
}

export async function createGroup(body: {
  id: string;
  name: string;
  creator_wallet: string;
  contribution_amt: number;
  max_members: number;
  swig_vault_addr?: string;
  on_chain_pda?: string;
}): Promise<Group> {
  const res = await fetch(`${API_BASE}/groups`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<Group>;
}

export async function joinGroup(
  groupId: string,
  body: { member_id: string; wallet_address: string; invite_code?: string; referrer_member_id?: string },
): Promise<{ member: Member; referral_applied: boolean }> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/join`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{ member: Member; referral_applied: boolean }>;
}

export async function contribute(
  groupId: string,
  body: {
    member_id: string;
    wallet_address: string;
    amount: number;
    cycle_number: number;
    tx_signature: string;
  },
): Promise<Record<string, unknown>> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/contribute`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<Record<string, unknown>>;
}

export async function buildTx(
  action:
    | "create-group"
    | "contribute"
    | "create-loan"
    | "vote-loan"
    | "create-swig-proposal"
    | "approve-swig-proposal"
    | "create-blink",
  body: Record<string, unknown>,
): Promise<TxPlan> {
  const res = await fetch(`${API_BASE}/tx/build/${encodeURIComponent(action)}`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<TxPlan>;
}

export async function runTreasuryAgent(body: { group_id?: string; dry_run?: boolean }): Promise<{
  run_at: string;
  dry_run: boolean;
  results: unknown[];
}> {
  const res = await fetch(`${API_BASE}/agent/run`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{ run_at: string; dry_run: boolean; results: unknown[] }>;
}

export async function fetchGroupExplorer(groupId: string): Promise<Record<string, string>> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/explorer`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<Record<string, string>>;
}

export function eventsWebSocketUrl(groupId?: string): string {
  const base = API_BASE.replace(/^http/, "ws");
  const q = groupId ? `?group_id=${encodeURIComponent(groupId)}` : "";
  return `${base.replace(/\/api\/v1$/, "")}/api/v1/ws${q}`;
}

export type GroupCycleResponse = {
  group_id: string;
  vault_balance: number;
  active_cycle: number;
  cycle_deadline?: string;
  payout_ready: boolean;
  last_settled_cycle: number;
  cycle_contribution: number;
  settlements: unknown[];
  settlement_note?: string;
};

export type YieldEventRow = {
  id: number;
  group_id: string;
  amount_deposited: number;
  protocol: string;
  apy: number;
  tx_signature: string;
  created_at: string;
};

export type GroupYieldResponse = {
  group_id: string;
  event_count: number;
  total_deposited: number;
  events: YieldEventRow[];
};

export type Web3BalanceResponse = {
  address: string;
  lamports: number;
  sol: number;
  solana_rpc_url: string;
};

export type BlinkActionRow = {
  id: string;
  blink_type: string;
  group_id: string;
  member_id: string;
  payload: string;
  status: string;
  created_at: string;
  updated_at: string;
};

export async function fetchGroupCycle(groupId: string): Promise<GroupCycleResponse> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/cycle`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<GroupCycleResponse>;
}

export async function fetchGroupYield(groupId: string): Promise<GroupYieldResponse> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/yield`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<GroupYieldResponse>;
}

export async function fetchMemberCreditScore(groupId: string, memberId: string): Promise<{ credit_score: number }> {
  const res = await fetch(
    `${API_BASE}/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(memberId)}/credit-score`,
    apiInit(),
  );
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{ credit_score: number }>;
}

export async function fetchWeb3Balance(address: string): Promise<Web3BalanceResponse> {
  const res = await fetch(`${API_BASE}/web3/balance?address=${encodeURIComponent(address)}`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<Web3BalanceResponse>;
}

export async function fetchBlinkActions(): Promise<BlinkActionRow[]> {
  const res = await fetch(`${API_BASE}/blinks/actions`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<BlinkActionRow[]>;
}

export async function createBlink(
  blinkType: "contribute" | "vote" | "withdraw",
  body: { group_id: string; member_id?: string; payload?: Record<string, unknown> },
): Promise<{ action_id: string; blink_type: string; url: string; status: string }> {
  const res = await fetch(`${API_BASE}/blinks/${encodeURIComponent(blinkType)}/create`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{ action_id: string; blink_type: string; url: string; status: string }>;
}

export type LoanRequestRow = {
  id: string;
  group_id: string;
  borrower_member_id: string;
  amount_lamports: number;
  reason: string;
  status: string;
  created_at: string;
};

export type SwigProposalRow = {
  id: string;
  group_id: string;
  title: string;
  kind: string;
  amount_lamports: number;
  approvals_required: number;
  approvals_count: number;
  status: string;
  created_by_member_id: string;
  created_at: string;
  executed_at?: string;
};

export async function fetchLoanRequests(groupId: string): Promise<LoanRequestRow[]> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/loans`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<LoanRequestRow[]>;
}

export async function createLoanRequest(
  groupId: string,
  body: { borrower_member_id: string; amount_lamports: number; reason?: string; id?: string },
): Promise<LoanRequestRow> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/loans`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<LoanRequestRow>;
}

export async function voteLoanRequest(
  groupId: string,
  loanId: string,
  body: { member_id: string; vote: "approve" | "reject" },
): Promise<{ loan_id: string; status: string; approvals: number; rejects: number }> {
  const res = await fetch(
    `${API_BASE}/groups/${encodeURIComponent(groupId)}/loans/${encodeURIComponent(loanId)}/vote`,
    apiInit({
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{ loan_id: string; status: string; approvals: number; rejects: number }>;
}

export async function fetchSwigProposals(groupId: string): Promise<SwigProposalRow[]> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/swig/proposals`, apiInit());
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<SwigProposalRow[]>;
}

export async function createSwigProposal(
  groupId: string,
  body: {
    title: string;
    kind?: string;
    amount_lamports: number;
    approvals_required?: number;
    created_by_member_id: string;
  },
): Promise<SwigProposalRow> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/swig/proposals`, apiInit({
    method: "POST",
    body: JSON.stringify(body),
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<SwigProposalRow>;
}

export async function approveSwigProposal(
  groupId: string,
  proposalId: string,
  body: { member_id: string; wallet: string },
): Promise<SwigProposalRow> {
  const res = await fetch(
    `${API_BASE}/groups/${encodeURIComponent(groupId)}/swig/proposals/${encodeURIComponent(proposalId)}/approve`,
    apiInit({
      method: "POST",
      body: JSON.stringify(body),
    }),
  );
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<SwigProposalRow>;
}

export async function endGroupCycle(groupId: string): Promise<{
  group_id: string;
  settled_cycle: number;
  new_active_cycle: number;
  payout_ready: boolean;
  vault_snapshot: number;
  total_cycle_pool: number;
  member_allocations: { member_id: string; wallet_address: string; share_lamports: number; contributed_in_cycle: number }[];
}> {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(groupId)}/cycle/end`, apiInit({
    method: "POST",
  }));
  if (!res.ok) throw new Error(await parseError(res));
  return res.json() as Promise<{
    group_id: string;
    settled_cycle: number;
    new_active_cycle: number;
    payout_ready: boolean;
    vault_snapshot: number;
    total_cycle_pool: number;
    member_allocations: { member_id: string; wallet_address: string; share_lamports: number; contributed_in_cycle: number }[];
  }>;
}

export function lamportsToSol(lamports: number): string {
  return (lamports / 1_000_000_000).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  });
}

export function solToLamports(sol: number): number {
  return Math.round(sol * 1_000_000_000);
}
