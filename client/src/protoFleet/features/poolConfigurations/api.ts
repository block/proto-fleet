import { useMemo } from "react";
import { createClient } from "@connectrpc/connect";

import { PoolConfigurationService } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { transport } from "@/protoFleet/api/transport";
import { useAccountScope } from "@/protoFleet/features/hostedAccounts/AccountScope";
import { createAccountTransport } from "@/protoFleet/features/hostedAccounts/api";
import { can } from "@/protoFleet/features/hostedAccounts/capabilities";
import { usePermissions } from "@/protoFleet/store";

const operatorClient = createClient(PoolConfigurationService, transport);

export const usePoolConfigurationClient = () => {
  const scope = useAccountScope();
  const accountId = scope?.account.accountId ?? "";
  const signal = scope?.signal;
  const readOnly = scope?.readOnly ?? false;
  const client = useMemo(
    () =>
      accountId && signal
        ? createClient(PoolConfigurationService, createAccountTransport(accountId, signal, { readOnly }))
        : operatorClient,
    [accountId, signal, readOnly],
  );
  return { client, accountId, readOnly };
};

export const usePoolConfigurationPermissions = () => {
  const scope = useAccountScope();
  const permissions = usePermissions();
  const change = scope ? can(scope.account, 4n) : permissions.includes("miner:update_pools");
  return {
    read: scope ? can(scope.account, 1n) : permissions.includes("pool:read"),
    manage: scope ? can(scope.account, 2n) : permissions.includes("pool:manage"),
    change,
    assign: change && !(scope?.readOnly ?? false),
  };
};
