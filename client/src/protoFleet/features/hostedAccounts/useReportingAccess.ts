import { useCallback } from "react";
import { ACCOUNT_READ_TIMEOUT_MS, hostedAccountsClient } from "./api";
import { useAccountQuery } from "./useAccountQuery";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";

export const useReportingAccess = (enabled = true) => {
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const load = useCallback(
    (signal: AbortSignal) =>
      hostedAccountsClient
        .listHostReportingAccounts({}, { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS })
        .then((data) => ({ generation, accounts: data.accounts })),
    [generation],
  );
  const query = useAccountQuery(load, enabled, true, POLL_INTERVAL_MS);
  return query.data?.generation === generation && !!query.data.accounts.length;
};
