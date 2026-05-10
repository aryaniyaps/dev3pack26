"use client";

import { PropsWithChildren } from 'react';
import { PrivyProvider as PrivyAuthProvider } from '@privy-io/react-auth';

interface PrivyProviderProps extends PropsWithChildren {
  appId?: string;
}

export function PrivyProvider({ appId, children }: PrivyProviderProps) {
  const configuredAppId = appId ?? process.env.NEXT_PUBLIC_PRIVY_APP_ID;
  const clientId = process.env.NEXT_PUBLIC_PRIVY_CLIENT_ID;

  if (!configuredAppId) {
    return <>{children}</>;
  }

  return (
    <PrivyAuthProvider
      appId={configuredAppId}
      {...(clientId ? { clientId } : {})}
      config={{
        loginMethods: ['email'],
        appearance: {
          walletChainType: 'solana-only',
        },
        embeddedWallets: {
          solana: {
            createOnLogin: 'users-without-wallets',
          },
        },
      }}
    >
      {children}
    </PrivyAuthProvider>
  );
}