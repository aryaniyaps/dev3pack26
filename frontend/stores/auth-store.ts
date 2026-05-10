import bs58 from 'bs58';
import nacl from 'tweetnacl';
import { Keypair } from '@solana/web3.js';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ??
  `${process.env.NEXT_PUBLIC_BACKEND_URL ?? 'http://localhost:8080'}/api/v1`;

/**
 * Ruby sessions use an HttpOnly `ruby_session` cookie set by the API on verify.
 * Zustand persists principal + UI hints (`auth-storage`); API calls use `credentials: "include"`.
 * If `/auth/me` fails for network/CORS reasons, keep local principal until the server returns 401/403.
 */
export class ApiRequestError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
  }
}

interface AuthPrincipal {
  session_id: string;
  user_id: string;
  wallet_address?: string;
  provider: string;
  expires_at: string;
}

interface AuthState {
  walletAddress: string | null;
  email: string | null;
  principal: AuthPrincipal | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  hasHydrated: boolean;
  error: string | null;
}

interface AuthActions {
  loginWithEmail: (email: string) => Promise<void>;
  loginWithWallet: () => Promise<void>;
  verifyPrivyToken: (privyToken: string, email?: string | null) => Promise<void>;
  refreshSession: (options?: { silent?: boolean }) => Promise<void>;
  logout: () => Promise<void>;
  setLoading: (loading: boolean) => void;
  setHasHydrated: (hasHydrated: boolean) => void;
}

type AuthStore = AuthState & AuthActions;

type AuthSessionResponse = {
  principal: AuthPrincipal;
};

type PhantomNonceResponse = {
  wallet_address: string;
  nonce: string;
  message: string;
};

/** Any injected wallet that can sign a SIWS-style message (Phantom, Backpack, etc.) */
type SolanaAuthProvider = {
  isPhantom?: boolean;
  connect: (opts?: { onlyIfTrusted?: boolean }) => Promise<{ publicKey: { toString: () => string } }>;
  signMessage: (
    message: Uint8Array,
    display?: 'utf8',
  ) => Promise<{ signature: Uint8Array }>;
};

declare global {
  interface Window {
    solana?: SolanaAuthProvider;
    /** Phantom documents this as the stable entrypoint; `window.solana` may be another wallet. */
    phantom?: { solana?: SolanaAuthProvider };
  }
}

function isSolanaAuthProvider(x: unknown): x is SolanaAuthProvider {
  if (!x || typeof x !== 'object') return false;
  const o = x as Record<string, unknown>;
  return typeof o.connect === 'function' && typeof o.signMessage === 'function';
}

function getSolanaAuthProvider(): SolanaAuthProvider | null {
  if (typeof window === 'undefined') return null;
  const phantom = window.phantom?.solana;
  if (isSolanaAuthProvider(phantom)) return phantom;
  const injected = window.solana;
  if (isSolanaAuthProvider(injected)) return injected;
  return null;
}

function allowEphemeralDevWalletLogin(): boolean {
  if (typeof window === 'undefined') return false;
  if (process.env.NEXT_PUBLIC_DISABLE_EPHEMERAL_WALLET_LOGIN === 'true') {
    return false;
  }
  if (process.env.NODE_ENV !== 'production') return true;
  const h = window.location.hostname;
  if (['localhost', '127.0.0.1', '::1'].includes(h)) return true;
  if (h.endsWith('.local')) return true;
  // Private LAN (e.g. next start + http://192.168.x.x:3000)
  if (/^10\.|^172\.(1[6-9]|2[0-9]|3[01])\.|^192\.168\./.test(h)) return true;
  return process.env.NEXT_PUBLIC_ALLOW_EPHEMERAL_WALLET_LOGIN === 'true';
}

function toSignatureBytes(sig: unknown): Uint8Array {
  if (sig instanceof Uint8Array) return sig;
  if (sig && typeof sig === 'object' && 'length' in sig && typeof (sig as ArrayLike<number>).length === 'number') {
    return new Uint8Array(sig as ArrayLike<number>);
  }
  throw new Error('Wallet returned an unexpected signature format');
}

async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const extraHeaders =
    init.headers && typeof init.headers === 'object' && !Array.isArray(init.headers)
      ? (init.headers as Record<string, string>)
      : {};
  const response = await fetch(`${API_BASE_URL}${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...extraHeaders,
    },
  });

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`;
    try {
      const payload = await response.json();
      if (typeof payload?.error === 'string') {
        message = payload.error;
      }
    } catch {
      // Keep the status-based fallback when the backend does not return JSON.
    }
    throw new ApiRequestError(message, response.status);
  }

  return response.json() as Promise<T>;
}

async function beginPhantomAuth(walletAddress: string) {
  return apiFetch<PhantomNonceResponse>('/auth/phantom/nonce', {
    method: 'POST',
    body: JSON.stringify({ wallet_address: walletAddress }),
  });
}

async function verifyPhantomAuth(payload: {
  wallet_address: string;
  nonce: string;
  message: string;
  signature: string;
}) {
  return apiFetch<AuthSessionResponse>('/auth/phantom/verify', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

function sessionState(session: AuthSessionResponse, email?: string | null) {
  return {
    principal: session.principal,
    walletAddress: session.principal.wallet_address || null,
    email: email ?? null,
    isAuthenticated: true,
    isLoading: false,
    error: null,
  };
}

async function signWithInjectedSolanaWallet() {
  const provider = getSolanaAuthProvider();
  if (!provider) {
    return null;
  }

  let res: { publicKey: { toString: () => string } };
  try {
    res = await provider.connect({ onlyIfTrusted: false });
  } catch {
    res = await provider.connect();
  }
  const walletAddress = res.publicKey.toString();

  const challenge = await beginPhantomAuth(walletAddress);
  const encodedMessage = new TextEncoder().encode(challenge.message);
  const signResult = await provider.signMessage(encodedMessage, 'utf8');
  const raw = (signResult as { signature?: unknown })?.signature;
  if (raw == null) {
    throw new Error('Wallet did not return a signature. Try again or update your wallet extension.');
  }
  const signature = toSignatureBytes(raw);

  return verifyPhantomAuth({
    wallet_address: walletAddress,
    nonce: challenge.nonce,
    message: challenge.message,
    signature: bs58.encode(signature),
  });
}

async function signWithLocalDevWallet() {
  if (!allowEphemeralDevWalletLogin()) {
    throw new Error(
      'No Solana wallet detected. Install Phantom (phantom.app), refresh this page, and approve the connection. If Phantom is installed, try disabling other wallet extensions that override window.solana.',
    );
  }

  const keypair = Keypair.generate();
  const walletAddress = keypair.publicKey.toBase58();
  const challenge = await beginPhantomAuth(walletAddress);
  const encodedMessage = new TextEncoder().encode(challenge.message);
  const signature = nacl.sign.detached(encodedMessage, keypair.secretKey);

  return verifyPhantomAuth({
    wallet_address: walletAddress,
    nonce: challenge.nonce,
    message: challenge.message,
    signature: bs58.encode(signature),
  });
}

export const useAuthStore = create<AuthStore>()(
  persist(
    (set) => ({
      walletAddress: null,
      email: null,
      principal: null,
      isAuthenticated: false,
      isLoading: false,
      hasHydrated: false,
      error: null,

      loginWithEmail: async (email: string) => {
        void email;
        set({ isLoading: false });
        throw new Error('Email login is handled by Privy. Configure NEXT_PUBLIC_PRIVY_APP_ID to enable it.');
      },

      loginWithWallet: async () => {
        set({ isLoading: true, error: null });
        try {
          let session: AuthSessionResponse;
          if (typeof window !== 'undefined') {
            const injected = await signWithInjectedSolanaWallet();
            if (injected) {
              session = injected;
            } else if (allowEphemeralDevWalletLogin()) {
              session = await signWithLocalDevWallet();
            } else {
              throw new Error(
                'No Solana wallet found. Open this app in a browser with the Phantom extension, unlock it, and click Connect again.',
              );
            }
          } else {
            session = await signWithLocalDevWallet();
          }
          set(sessionState(session));
        } catch (error) {
          const message = error instanceof Error ? error.message : 'Wallet login failed';
          set({ isLoading: false, error: message });
          throw error;
        }
      },

      verifyPrivyToken: async (privyToken: string, email?: string | null) => {
        set({ isLoading: true, error: null });
        try {
          const session = await apiFetch<AuthSessionResponse>('/auth/privy/verify', {
            method: 'POST',
            body: JSON.stringify({ privy_token: privyToken }),
          });
          set(sessionState(session, email));
        } catch (error) {
          const message = error instanceof Error ? error.message : 'Privy verification failed';
          set({ isLoading: false, error: message });
          throw error;
        }
      },

      refreshSession: async (options?: { silent?: boolean }) => {
        const silent = options?.silent ?? false;
        if (silent) {
          set({ error: null });
        } else {
          set({ isLoading: true, error: null });
        }
        try {
          const principal = await apiFetch<AuthPrincipal>('/auth/me', { method: 'GET' });
          set({
            principal,
            walletAddress: principal.wallet_address || null,
            isAuthenticated: true,
            isLoading: false,
            error: null,
          });
        } catch (error) {
          const status = error instanceof ApiRequestError ? error.status : 0;
          const sessionRejected = status === 401 || status === 403;
          if (sessionRejected) {
            set({
              principal: null,
              walletAddress: null,
              email: null,
              isAuthenticated: false,
              isLoading: false,
              error: silent ? null : error instanceof Error ? error.message : 'Session refresh failed',
            });
          } else {
            set({
              isLoading: false,
              error:
                silent
                  ? null
                  : error instanceof Error
                    ? error.message
                    : 'Session refresh failed',
            });
          }
        }
      },

      logout: async () => {
        try {
          await apiFetch('/auth/logout', { method: 'POST' });
        } catch {
          // Local logout should still clear stale or already-revoked sessions.
        }
        set({
          walletAddress: null,
          email: null,
          principal: null,
          isAuthenticated: false,
          isLoading: false,
          error: null,
        });
      },

      setLoading: (loading: boolean) => set({ isLoading: loading }),
      setHasHydrated: (hasHydrated: boolean) => set({ hasHydrated }),
    }),
    {
      name: 'auth-storage',
      merge: (persistedState, currentState) => {
        const p = (persistedState ?? {}) as Partial<AuthState & { token?: string | null }>;
        const { token: _legacyToken, ...rest } = p;
        const merged = {
          ...currentState,
          ...rest,
        };
        if (merged.principal) {
          merged.isAuthenticated = true;
        }
        return merged;
      },
      onRehydrateStorage: () => (state) => {
        state?.setHasHydrated(true);
        void state?.refreshSession({ silent: true });
      },
      partialize: (state) => ({
        walletAddress: state.walletAddress,
        email: state.email,
        principal: state.principal,
        isAuthenticated: state.isAuthenticated,
      }),
    }
  )
);