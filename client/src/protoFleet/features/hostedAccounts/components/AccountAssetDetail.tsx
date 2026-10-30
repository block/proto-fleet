import { useCallback, useEffect } from "react";
import { useParams } from "react-router-dom";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { accountPath } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountAssetCell } from "./AccountAssetCell";
import { assetColumnTitles, assetKindLabel } from "./accountAssetColumns";
import { AccountState } from "./AccountState";
import { MiningAssetKind } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Row from "@/shared/components/Row";

const AccountAssetDetail = () => {
  const { account, client } = useRequiredAccountScope();
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
        <>
          <SettingsPageHeader
            title={asset.name || assetKindLabel(assetKind)}
            description="Current allocated mining asset"
          />
          <div>
            {(
              ["kind", "model", "status", "hashrate", "powerUsage", "efficiency", "temperature", "container"] as const
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
        </>
      ) : !query.loading && !query.error ? (
        <p>
          This mining asset is unavailable in this account. Retained chart history does not grant access to current
          details.
        </p>
      ) : null}
    </div>
  );
};
export default AccountAssetDetail;
