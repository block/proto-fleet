import { type ReactNode, useCallback, useEffect, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { ACCOUNT_READ_TIMEOUT_MS, isRetryableReadError } from "./api";
import { type Directory, HostedAccountsContext } from "./HostedAccountsContext";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { hostedAccountsClient } from "@/protoFleet/features/hostedAccounts/api";
import { useFleetStore } from "@/protoFleet/store";

export const HostedAccountsProvider = ({ children }: { children: ReactNode }) => {
  const authenticated = useFleetStore((s) => s.auth.isAuthenticated);
  const generation = useFleetStore((s) => s.auth.sessionGeneration);
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ generation: number; data: Directory | null; error: string | null } | null>(
    null,
  );
  const refresh = useCallback(() => setRevision((r) => r + 1), []);
  useEffect(() => {
    if (!authenticated) return;
    const controller = new AbortController();
    hostedAccountsClient.listAccounts({}, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal: controller.signal }).then(
      (data) => {
        if (!controller.signal.aborted) setResult({ generation, data, error: null });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) {
          setResult((previous) => ({
            generation,
            data: isRetryableReadError(error) && previous?.generation === generation ? previous.data : null,
            error: getErrorMessage(error),
          }));
          if (
            ConnectError.from(error).code === Code.Unauthenticated &&
            useFleetStore.getState().auth.sessionGeneration === generation
          )
            useFleetStore.getState().auth.logout();
        }
      },
    );
    return () => controller.abort();
  }, [authenticated, generation, revision]);
  useEffect(() => {
    if (!authenticated) return;
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [authenticated, refresh]);
  // Never render a prior login's directory while a new session is loading.
  const current = authenticated && result?.generation === generation ? result : null;
  return (
    <HostedAccountsContext.Provider
      value={{
        accounts: current?.data?.accounts ?? [],
        hostAccess: current?.data?.hostAccess ?? false,
        loading: authenticated && !current,
        error: current?.error ?? null,
        refresh,
      }}
    >
      {children}
    </HostedAccountsContext.Provider>
  );
};
