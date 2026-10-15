import { useCallback, useEffect, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { useRequiredAccountScope } from "../AccountScope";
import { accountPath, can } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import type { AccountMiningBucket, MiningSummary } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import Stats from "@/shared/components/Stats";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { formatEfficiencyOrDash, formatHashrateOrDash, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

const count = (value: bigint) => value.toLocaleString();
const quantity = (value: bigint, name: string) => `${count(value)} ${name}${value === 1n ? "" : "s"}`;
const reporting = (value: bigint, total: bigint) => `${count(value)} of ${quantity(total, "asset")} reporting`;
const hash = (summary?: MiningSummary) =>
  formatHashrateOrDash(summary?.hashrateHs === undefined ? null : summary.hashrateHs / 1e12);
const power = (summary?: MiningSummary) =>
  formatPowerKwOrDash(summary?.powerW === undefined ? null : summary.powerW / 1000);

const AccountOverview = () => {
  const { account, client } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const [scope, setScope] = useState("all");
  const load = useCallback(
    (signal: AbortSignal) => client.getAccountMiningSummary({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const query = useAccountQuery(load);
  useEffect(() => {
    window.addEventListener("focus", query.refresh);
    return () => window.removeEventListener("focus", query.refresh);
  }, [query.refresh]);
  const partitions = (can(account, 8n) || can(account, 16n)) && !!query.data?.direct;
  const summary = partitions && scope === "direct" ? query.data?.direct : query.data?.totals;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col">
        <div className="min-w-0 flex-1">
          <SettingsPageHeader title="Overview" description={account.name} />
        </div>
        <div className="flex flex-wrap gap-3">
          <Button variant="secondary" text="Refresh" onClick={query.refresh} disabled={query.loading} />
          <Button variant="primary" text="View mining assets" to={accountPath(account.accountId, "assets")} />
        </div>
      </div>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          {partitions ? (
            <TabStrip ariaLabel="Mining summary scope" activeId={scope} onSelect={setScope}>
              <TabStripItem id="all" label="All account assets" />
              <TabStripItem id="direct" label="Directly assigned" />
            </TabStrip>
          ) : null}
          <p className="text-300 text-text-primary-70">
            {partitions && scope === "direct"
              ? "Assets assigned directly to this account, excluding subclients."
              : account.depth < 2
                ? "All assets allocated to this account, including subclients."
                : "Assets allocated to this account."}
          </p>
          {summary ? (
            <>
              <Stats
                stats={[
                  {
                    label: "Mining assets",
                    value: count(summary.assetCount),
                    preformatted: true,
                    text: `${quantity(summary.minerCount, "miner")} · ${quantity(summary.moduleCount, "module")}`,
                  },
                  {
                    label: "Hashrate",
                    value: hash(summary),
                    preformatted: true,
                    text: reporting(summary.hashrateReportingCount, summary.assetCount),
                  },
                  {
                    label: "Equipment power",
                    value: power(summary),
                    preformatted: true,
                    text: reporting(summary.powerReportingCount, summary.assetCount),
                  },
                  {
                    label: "Efficiency",
                    value: formatEfficiencyOrDash(summary.efficiencyJTh ?? null),
                    preformatted: true,
                    text: reporting(summary.pairedReportingCount, summary.assetCount),
                    tooltipContent: "Weighted across assets reporting both hashrate and equipment power.",
                  },
                ]}
              />
              {summary.assetCount === 0n ? (
                <SettingsEmptyState
                  title="No mining assets assigned"
                  description="Assigned miners and modules will appear here. Ask your account administrator about allocations."
                />
              ) : (
                <>
                  <section aria-label="Mining asset states" className="flex flex-wrap gap-x-6 gap-y-2 text-300">
                    <span>{count(summary.hashingCount)} Hashing</span>
                    <span>{count(summary.attentionCount)} Needs attention</span>
                    <span>{count(summary.offlineCount)} Offline</span>
                    <span>{count(summary.sleepingCount)} Sleeping</span>
                    <span>{count(summary.unknownCount)} Unknown</span>
                  </section>
                  {summary.partialModuleCount > 0n || summary.unknownModuleCount > 0n ? (
                    <p className="text-300 text-text-primary-70">
                      Module measurement coverage: {quantity(summary.partialModuleCount, "module")} partial ·{" "}
                      {quantity(summary.unknownModuleCount, "module")} unknown. Reporting counts can include partial
                      module readings.
                    </p>
                  ) : null}
                  {summary.unavailableCount > 0n || summary.outsidePolicyCount > 0n ? (
                    <p className="text-300 text-text-primary-70">
                      {count(summary.unavailableCount)} unavailable · {count(summary.outsidePolicyCount)} outside
                      account site policy. These allocations remain in asset totals; unavailable readings are excluded.
                    </p>
                  ) : null}
                </>
              )}
            </>
          ) : (
            <SettingsEmptyState
              title="Mining summary unavailable"
              description="Refresh to try loading this account’s current report again."
            />
          )}
          {partitions && query.data.children.length > 0 ? (
            <section className="flex flex-col gap-4" aria-label="Subclient comparison">
              <h2 className="text-heading-100">By subclient</h2>
              <p className="text-300 text-text-primary-70">
                Direct assignments are separate from subclients. Suspended accounts remain included in totals.
              </p>
              <List<AccountMiningBucket, string, "name" | "assets" | "hashrate" | "power" | "efficiency">
                items={[
                  {
                    $typeName: "hosted.v1.AccountMiningBucket",
                    accountId: account.accountId,
                    name: "Directly assigned",
                    suspended: false,
                    summary: query.data.direct,
                  },
                  ...query.data.children,
                ]}
                itemKey="accountId"
                itemSelectable={false}
                stickyFirstColumn={false}
                activeCols={["name", "assets", "hashrate", "power", "efficiency"]}
                colTitles={{
                  name: "Account",
                  assets: "Mining assets",
                  hashrate: "Hashrate",
                  power: "Equipment power",
                  efficiency: "Efficiency",
                }}
                colConfig={{
                  name: {
                    width: "min-w-48",
                    component: (bucket) => {
                      const readable =
                        bucket.accountId !== account.accountId &&
                        !bucket.suspended &&
                        directory?.accounts.some((child) => child.accountId === bucket.accountId && can(child, 1n));
                      return (
                        <div>
                          {readable ? (
                            <Button
                              variant="textOnly"
                              textColor="text-text-primary"
                              textOnlyUnderlineOnHover={false}
                              text={bucket.name}
                              to={accountPath(bucket.accountId)}
                            />
                          ) : (
                            bucket.name
                          )}
                          {bucket.suspended ? <div className="text-200 text-text-primary-50">Suspended</div> : null}
                        </div>
                      );
                    },
                  },
                  assets: {
                    width: "min-w-28",
                    component: (bucket) => (bucket.summary ? count(bucket.summary.assetCount) : "—"),
                  },
                  hashrate: {
                    width: "min-w-48",
                    component: (bucket) => (
                      <div>
                        {hash(bucket.summary)}
                        {bucket.summary ? (
                          <div className="text-200 text-text-primary-50">
                            {reporting(bucket.summary.hashrateReportingCount, bucket.summary.assetCount)}
                          </div>
                        ) : null}
                      </div>
                    ),
                  },
                  power: {
                    width: "min-w-48",
                    component: (bucket) => (
                      <div>
                        {power(bucket.summary)}
                        {bucket.summary ? (
                          <div className="text-200 text-text-primary-50">
                            {reporting(bucket.summary.powerReportingCount, bucket.summary.assetCount)}
                          </div>
                        ) : null}
                      </div>
                    ),
                  },
                  efficiency: {
                    width: "min-w-48",
                    component: (bucket) => (
                      <div>
                        {formatEfficiencyOrDash(bucket.summary?.efficiencyJTh ?? null)}
                        {bucket.summary ? (
                          <div className="text-200 text-text-primary-50">
                            {reporting(bucket.summary.pairedReportingCount, bucket.summary.assetCount)}
                          </div>
                        ) : null}
                      </div>
                    ),
                  },
                }}
              />
            </section>
          ) : null}
          <div className="space-y-2 text-200 text-text-primary-50">
            <p>
              Current reporting uses fresh readings where available; report time is not the sample time. Missing
              readings are shown as —. Equipment power excludes container plant power. Efficiency uses assets reporting
              both metrics.
            </p>
            {query.data.generatedAt ? (
              <p>Report generated {timestampDate(query.data.generatedAt).toLocaleString()}.</p>
            ) : null}
            <p>Allocations control reporting. They do not apply pools or transfer equipment control.</p>
          </div>
        </>
      ) : null}
    </div>
  );
};
export default AccountOverview;
