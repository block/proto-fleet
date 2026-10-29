import { useCallback, useEffect, useMemo, useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";
import { accountDashboardData, reportingCoverage } from "../accountDashboardData";
import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS, isRetryableReadError } from "../api";
import { accountPath, can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import DashboardPerformance from "@/protoFleet/features/dashboard/components/DashboardPerformance";
import FleetHealthMetrics from "@/protoFleet/features/dashboard/components/FleetHealthMetrics";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { useTemperatureUnit } from "@/protoFleet/store";
import Button, { sizes } from "@/shared/components/Button";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import Stats from "@/shared/components/Stats";
import {
  convertCtoF,
  formatEfficiencyOrDash,
  formatHashrateOrDash,
  formatPowerKwOrDash,
} from "@/shared/utils/telemetryFormat";

const AccountOverview = () => {
  const { account, client } = useRequiredAccountScope();
  const temperatureUnit = useTemperatureUnit();
  const [duration, setDuration] = useState<FleetDuration>("1h");
  const [ownerAccountId, setOwnerAccountId] = useState("");
  const [siteId, setSiteId] = useState("");
  const [filters, setFilters] = useState({
    sites: [] as { siteId: bigint; name: string }[],
    children: [] as { accountId: string; name: string; suspended: boolean }[],
  });
  const showAllocations = can(account, 8n) || can(account, 16n);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const end = new Date();
      try {
        const result = await client.getAccountDashboard(
          {
            accountId: account.accountId,
            ownerAccountId: showAllocations && ownerAccountId ? ownerAccountId : undefined,
            siteId: siteId ? BigInt(siteId) : undefined,
            startTime: timestampFromDate(new Date(end.getTime() - getFleetDurationMs(duration))),
            endTime: timestampFromDate(end),
            bucketSeconds: getGranularityForDuration(duration),
          },
          { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
        );
        if (!signal.aborted) {
          setFilters({
            sites: result.sites.map(({ siteId, name }) => ({ siteId, name })),
            children: result.children.map(({ accountId, name, suspended }) => ({ accountId, name, suspended })),
          });
        }
        return result;
      } catch (error) {
        if (!signal.aborted && !isRetryableReadError(error)) setFilters({ sites: [], children: [] });
        throw error;
      }
    },
    [account.accountId, client, duration, ownerAccountId, showAllocations, siteId],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  useEffect(() => {
    window.addEventListener("focus", query.refresh);
    return () => {
      window.removeEventListener("focus", query.refresh);
    };
  }, [query.refresh]);
  const reportFilters: FilterChipsBarFilter[] = [
    ...(showAllocations
      ? [
          {
            key: "allocation",
            title: "Allocation",
            options: [
              { id: account.accountId, label: "Directly managed" },
              ...filters.children.map((child) => ({
                id: child.accountId,
                label: `${child.name}${child.suspended ? " (suspended)" : ""}`,
              })),
              ...(ownerAccountId &&
              ownerAccountId !== account.accountId &&
              !filters.children.some((child) => child.accountId === ownerAccountId)
                ? [{ id: ownerAccountId, label: "Unavailable allocation" }]
                : []),
            ],
            selectedValues: ownerAccountId ? [ownerAccountId] : [],
          },
        ]
      : []),
    {
      key: "site",
      title: "Site",
      options: [
        ...filters.sites.map((site) => ({ id: site.siteId.toString(), label: site.name })),
        ...(siteId && !filters.sites.some((site) => site.siteId.toString() === siteId)
          ? [{ id: siteId, label: "Unavailable site" }]
          : []),
      ],
      selectedValues: siteId ? [siteId] : [],
    },
  ];
  const summary = query.data?.totals;
  const history = useMemo(() => (query.data ? accountDashboardData(query.data.readings) : undefined), [query.data]);
  const historyHasReadings = query.data?.readings.some(
    ({ summary: reading, uptimeStatus }) =>
      (uptimeStatus && uptimeStatus.hashingCount + uptimeStatus.brokenCount + uptimeStatus.notHashingCount > 0) ||
      (reading &&
        (reading.hashrateHs !== undefined ||
          reading.powerW !== undefined ||
          reading.temperatureC !== undefined ||
          reading.uptimePercent !== undefined)),
  );
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-heading-300">Overview</h1>
        <div className="flex flex-wrap gap-3">
          <Button
            variant="primary"
            size={sizes.compact}
            text="View mining assets"
            to={accountPath(account.accountId, "assets")}
          />
        </div>
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <FilterChipsBar
          filters={reportFilters}
          onChange={(key, values) => {
            const selected = values.slice(-1)[0] ?? "";
            if (key === "allocation") setOwnerAccountId(selected);
            if (key === "site") setSiteId(selected);
          }}
          onClearAll={() => {
            setOwnerAccountId("");
            setSiteId("");
          }}
        />
      </div>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {summary ? (
        <>
          <section aria-label="Fleet health">
            <FleetHealthMetrics
              assetLabel="asset"
              fleetSize={Number(summary.assetCount)}
              healthyMiners={Number(summary.hashingCount)}
              needsAttentionMiners={Number(summary.attentionCount)}
              offlineMiners={Number(summary.offlineCount)}
              sleepingMiners={Number(summary.sleepingCount)}
            />
            {summary.unknownCount > 0n ? (
              <p className="mt-3 text-300 text-text-primary-70">
                {summary.unknownCount.toLocaleString()} assets with unknown state
              </p>
            ) : null}
          </section>
          <section aria-label="Current readings">
            <Stats
              grid="grid-cols-2 laptop:grid-cols-5"
              stats={[
                {
                  label: "Hashrate",
                  value: formatHashrateOrDash(summary.hashrateHs === undefined ? null : summary.hashrateHs / 1e12),
                  preformatted: true,
                  text: reportingCoverage(summary.hashrateReportingCount, summary),
                },
                {
                  label: "Power",
                  value: formatPowerKwOrDash(summary.powerW === undefined ? null : summary.powerW / 1000),
                  preformatted: true,
                  text: reportingCoverage(summary.powerReportingCount, summary),
                },
                {
                  label: "Efficiency",
                  value: formatEfficiencyOrDash(summary.efficiencyJTh ?? null),
                  preformatted: true,
                },
                {
                  label: "Temperature",
                  value:
                    summary.temperatureC === undefined
                      ? "—"
                      : `${(temperatureUnit === "F" ? convertCtoF(summary.temperatureC) : summary.temperatureC).toFixed(1)} °${temperatureUnit}`,
                  preformatted: true,
                  text: reportingCoverage(summary.temperatureReportingCount, summary),
                },
                {
                  label: "Uptime",
                  value: summary.uptimePercent === undefined ? "—" : `${summary.uptimePercent.toFixed(1)}%`,
                  preformatted: true,
                  text: reportingCoverage(summary.uptimeReportingCount, summary),
                },
              ]}
            />
            {summary.partialModuleCount > 0n || summary.unknownModuleCount > 0n ? (
              <p className="mt-2 text-300 text-text-primary-70">
                Module measurement coverage: {summary.partialModuleCount.toLocaleString()} partial ·{" "}
                {summary.unknownModuleCount.toLocaleString()} unknown. Power includes only measured equipment readings.
              </p>
            ) : null}
            {summary.unavailableCount > 0n || summary.outsidePolicyCount > 0n ? (
              <p className="mt-2 text-300 text-text-primary-70">
                {summary.unavailableCount.toLocaleString()} unavailable · {summary.outsidePolicyCount.toLocaleString()}{" "}
                outside account site policy. These allocations remain in asset totals; unavailable readings are
                excluded.
              </p>
            ) : null}
          </section>
          {summary.assetCount === 0n ? (
            <SettingsEmptyState
              title="No mining assets"
              description="Previously authorized readings can remain available below after recall, subject to current site permissions and telemetry retention."
            />
          ) : null}
        </>
      ) : null}
      {query.data ? (
        <>
          {query.data.sites.length > 0 ? (
            <section aria-label="Allocated sites" className="flex flex-wrap gap-3">
              {query.data.sites.map((site) => (
                <Button
                  key={site.siteId.toString()}
                  variant="secondary"
                  size={sizes.compact}
                  text={`${site.name} · ${site.summary?.assetCount.toLocaleString() ?? "0"} assets`}
                  onClick={() => setSiteId(site.siteId.toString())}
                />
              ))}
            </section>
          ) : null}
          {!historyHasReadings ? (
            <p className="text-300 text-text-primary-70">
              No authorized historical readings in this time range. Unknown allocation periods and missing telemetry are
              unavailable.
            </p>
          ) : null}
        </>
      ) : null}
      {query.loading ||
      query.data ||
      (query.errorCode !== null &&
        query.errorCode !== Code.PermissionDenied &&
        query.errorCode !== Code.Unauthenticated) ? (
        <div className="-mx-10 phone:-mx-6">
          <DashboardPerformance
            duration={duration}
            onDurationChange={setDuration}
            metrics={history?.metrics}
            totalAssets={Number(summary?.assetCount ?? 0n)}
            accountReadings
            temperatureStatusCounts={history?.temperatureStatusCounts}
            uptimeStatusCounts={history?.uptimeStatusCounts}
            uptimeExpectedAssetCount={history?.uptimeExpectedAssetCount}
            tooltipNotes={history?.tooltipNotes}
          />
        </div>
      ) : null}
    </div>
  );
};
export default AccountOverview;
