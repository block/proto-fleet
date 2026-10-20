import { useCallback, useEffect, useMemo } from "react";

import { createAccountConnection } from "./api";
import { can } from "./capabilities";
import { useHostedAccounts } from "./HostedAccountsContext";
import { useAccountQuery } from "./useAccountQuery";
import { MiningAssetKind, type MiningAssetReference } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";

export type AssetReference = Pick<MiningAssetReference, "kind" | "id">;
export const allocationKey = (asset: AssetReference) => `${asset.kind}:${asset.id}`;

export const useFleetAccountInventory = (assets: AssetReference[], refreshOnFocus = false) => {
  const directory = useHostedAccounts();
  const root = directory?.accounts.find((account) => account.depth === 0 && can(account, 16n));
  const accountId = root?.accountId ?? "";
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const { connection } = useMemo(
    () => ({ generation, connection: createAccountConnection(accountId) }),
    [accountId, generation],
  );
  useEffect(() => {
    connection.activate();
    return () => connection.dispose();
  }, [connection]);
  const keys = assets.map(allocationKey).sort().join(",");
  const load = useCallback(
    (signal: AbortSignal) =>
      connection.client.listAccountAssets(
        {
          accountId,
          selectedAssets: keys
            .split(",")
            .filter(Boolean)
            .map((key) => {
              const [kind, id] = key.split(":");
              return { kind: Number(kind) as MiningAssetKind, id: BigInt(id) };
            }),
        },
        { signal },
      ),
    [accountId, connection, keys],
  );
  const query = useAccountQuery(load, !!accountId && !!keys, true);
  useEffect(() => {
    if (!refreshOnFocus || !accountId || !keys) return;
    window.addEventListener("focus", query.refresh);
    return () => window.removeEventListener("focus", query.refresh);
  }, [refreshOnFocus, accountId, keys, query.refresh]);
  const byAsset = useMemo(
    () => new Map(query.data?.assets.map((asset) => [allocationKey(asset), asset])),
    [query.data],
  );
  return { ...query, root, client: connection.client, byAsset };
};
