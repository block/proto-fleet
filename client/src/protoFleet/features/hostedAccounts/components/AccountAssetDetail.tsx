import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router-dom";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { accountPath, can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountAssetCell } from "./AccountAssetCell";
import { assetColumnTitles, assetKindLabel } from "./accountAssetColumns";
import { AccountState } from "./AccountState";
import { MiningAssetKind } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { PoolAssignments } from "@/protoFleet/features/poolConfigurations/PoolAssignments";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button from "@/shared/components/Button";
import Row from "@/shared/components/Row";

const AccountAssetDetail = () => {
  const { account, client, readOnly } = useRequiredAccountScope();
  const [changingPools, setChangingPools] = useState(false);
  const { kind, assetId = "" } = useParams();
  const valid = (kind === "miner" || kind === "module" || kind === "hashboard") && /^[1-9]\d*$/.test(assetId);
  const assetKind =
    kind === "module"
      ? MiningAssetKind.MODULE
      : kind === "hashboard"
        ? MiningAssetKind.HASHBOARD
        : MiningAssetKind.MINER;
  const load = useCallback(
    (signal: AbortSignal) =>
      client.getAccountAssetDetail(
        {
          accountId: account.accountId,
          asset: {
            kind: assetKind,
            id: BigInt(assetId),
          },
        },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      ),
    [account.accountId, client, assetKind, assetId],
  );
  const query = useAccountQuery(load, valid, true, POLL_INTERVAL_MS);
  useEffect(() => {
    if (!valid) return;
    window.addEventListener("focus", query.refresh);
    return () => {
      window.removeEventListener("focus", query.refresh);
    };
  }, [valid, query.refresh]);
  const asset = query.data?.asset;
  const canChangePools = !!asset?.canChangePools && can(account, 4n) && !readOnly;
  if (changingPools && !canChangePools) setChangingPools(false);
  return (
    <div className="flex flex-col gap-6">
      <Breadcrumb
        segments={[
          { label: "Mining assets", to: accountPath(account.accountId, "assets") },
          { label: asset?.name || "Asset details" },
        ]}
      />
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {asset ? (
        <PoolAssignments
          deviceIds={asset.kind === MiningAssetKind.MINER ? [asset.id] : []}
          moduleIds={asset.kind === MiningAssetKind.MODULE ? [asset.id] : []}
          hashboardIds={asset.kind === MiningAssetKind.HASHBOARD ? [asset.id] : []}
        >
          <SettingsPageHeader
            title={asset.name || assetKindLabel(assetKind)}
            description="Current allocated mining asset"
          />
          {canChangePools ? (
            <div>
              <Button variant="secondary" text="Edit pool" onClick={() => setChangingPools(true)} />
            </div>
          ) : null}
          <div>
            {(
              [
                "kind",
                "model",
                "status",
                "poolConfiguration",
                "hashrate",
                "powerUsage",
                "efficiency",
                "temperature",
                "container",
              ] as const
            ).map((column) => (
              <Row key={column} className="flex items-center justify-between gap-4">
                <span>
                  {column === "powerUsage" && asset.kind === MiningAssetKind.MODULE
                    ? "Module/PSU power"
                    : assetColumnTitles[column]}
                </span>
                <AccountAssetCell asset={asset} column={column} />
              </Row>
            ))}
          </div>
        </PoolAssignments>
      ) : !query.loading && !query.error ? (
        <p>
          This mining asset is unavailable in this account. Retained chart history does not grant access to current
          details.
        </p>
      ) : null}
      {asset && changingPools && canChangePools ? (
        <ConfigurationSelector
          deviceIds={asset.kind === MiningAssetKind.MINER ? [asset.id] : []}
          boards={
            asset.kind === MiningAssetKind.MODULE
              ? { moduleIds: [asset.id] }
              : asset.kind === MiningAssetKind.HASHBOARD
                ? { hashboardIds: [asset.id] }
                : undefined
          }
          onDismiss={() => setChangingPools(false)}
          onApplied={query.refresh}
        />
      ) : null}
    </div>
  );
};
export default AccountAssetDetail;
