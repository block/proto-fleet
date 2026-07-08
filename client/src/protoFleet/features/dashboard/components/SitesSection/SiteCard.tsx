import { useMemo, useRef } from "react";
import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useSiteStats } from "@/protoFleet/api/useSiteStats";
import { HealthBar } from "@/protoFleet/components/HealthBar";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { encodeFilterToURL } from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert, ArrowRight } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Metric from "@/shared/components/Metric";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useInViewport } from "@/shared/hooks/useInViewport";
import { convertCtoF, formatHashrate, formatPowerMwOrDash } from "@/shared/utils/telemetryFormat";

// Miner statuses that count as "needs attention" — mirrors the FleetHealth
// segment so the card's badge and the filtered miner list agree.
const NEEDS_ATTENTION_STATUSES = [
  DeviceStatus.ERROR,
  DeviceStatus.NEEDS_MINING_POOL,
  DeviceStatus.UPDATING,
  DeviceStatus.REBOOT_REQUIRED,
];

// Integer-rounded temperature range (e.g. "30–60 °C"). The shared
// formatTempRange renders one decimal per bound, which wraps to two lines in
// the narrow card column — here we round and share a single unit suffix.
const formatTempRangeRounded = (minC: number, maxC: number, unit: "C" | "F"): string => {
  const toUnit = (c: number) => (unit === "F" ? convertCtoF(c) : c);
  return `${Math.round(toUnit(minC))}–${Math.round(toUnit(maxC))} °${unit}`;
};

interface SiteCardProps {
  site: SiteWithCounts;
  className?: string;
}

// A single site tile in the All-Sites gallery: name + quick actions, a row
// of small telemetry metrics, and the fleet-health bar. Stats are polled
// per card via GetSiteStats — every card in the gallery mounts (the track
// renders them all and slides), so this fans out one poll per site.
const SiteCard = ({ site, className }: SiteCardProps) => {
  const id = site.site?.id ?? 0n;
  const idText = id.toString();
  const slug = site.site?.slug ?? "";
  const label = site.site?.name ?? "(unnamed site)";
  const temperatureUnit = useTemperatureUnit();

  // Viewport-gate the poll: the carousel keeps every site card mounted, so
  // without this an org with N sites fires N GetSiteStats polls every tick.
  // Off-screen cards suspend; useSiteStats keeps their last-good stats so
  // re-scrolling doesn't flash a skeleton.
  const cardRef = useRef<HTMLDivElement | null>(null);
  const isVisible = useInViewport(cardRef);

  const { stats } = useSiteStats({ siteId: id, enabled: id !== 0n && isVisible, pollIntervalMs: POLL_INTERVAL_MS });

  const total = stats ? stats.hashingCount + stats.brokenCount + stats.offlineCount + stats.sleepingCount : 0;
  const needsAttentionPct = total > 0 && stats ? Math.round((stats.brokenCount / total) * 100) : 0;

  // Deep-link to the miner list filtered to this site's needs-attention
  // devices. Scoped to the card's own site so the destination matches the
  // badge regardless of the topbar selection.
  const needsAttentionHref = useMemo(() => {
    const params = encodeFilterToURL(create(MinerListFilterSchema, { deviceStatus: NEEDS_ATTENTION_STATUSES }));
    return scopedPath(`/fleet/miners?${params.toString()}`, { kind: "site", id: idText, slug });
  }, [idText, slug]);

  // undefined = loading (Metric shows a skeleton), null = loaded but not
  // reporting (Metric shows an em-dash).
  const hashrateValue =
    stats === undefined ? undefined : stats.hashrateReportingCount > 0 ? formatHashrate(stats.totalHashrateThs) : null;
  const powerValue =
    stats === undefined ? undefined : stats.powerReportingCount > 0 ? formatPowerMwOrDash(stats.totalPowerKw) : null;
  const temperatureValue =
    stats === undefined
      ? undefined
      : stats.temperatureReportingCount > 0
        ? formatTempRangeRounded(stats.minTemperatureC, stats.maxTemperatureC, temperatureUnit)
        : null;

  return (
    <div
      ref={cardRef}
      className={clsx("flex h-full flex-col gap-5 rounded-xl bg-surface-elevated-base p-10 shadow-100", className)}
      data-testid={`dashboard-site-card-${idText}`}
    >
      <div className="flex items-start justify-between gap-2">
        <span
          className="min-w-0 truncate text-heading-300 text-text-primary"
          data-testid={`dashboard-site-card-${idText}-name`}
        >
          {label}
        </span>
        <div className="flex shrink-0 items-center gap-2">
          {stats && stats.brokenCount > 0 ? (
            <Button
              to={needsAttentionHref}
              variant={variants.secondaryDanger}
              size={sizes.compact}
              prefixIcon={<Alert width="w-4" />}
              text={`${needsAttentionPct}% need attention`}
              testId={`dashboard-site-card-${idText}-needs-attention`}
            />
          ) : null}
          <Button
            to={`/sites/${idText}`}
            variant={variants.secondary}
            size={sizes.compact}
            ariaLabel={`View ${label} details`}
            prefixIcon={<ArrowRight width="w-4" />}
            testId={`dashboard-site-card-${idText}-detail`}
          />
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <Metric
          label="Hashrate"
          value={hashrateValue}
          variant="compact"
          testId={`dashboard-site-card-${idText}-hashrate`}
        />
        <Metric label="Power" value={powerValue} variant="compact" testId={`dashboard-site-card-${idText}-power`} />
        <Metric
          label="Temperature"
          value={temperatureValue}
          variant="compact"
          testId={`dashboard-site-card-${idText}-temperature`}
        />
      </div>

      {stats === undefined ? (
        <SkeletonBar className="h-1.5 w-full" />
      ) : (
        <HealthBar
          healthy={stats.hashingCount}
          needsAttention={stats.brokenCount}
          offline={stats.offlineCount}
          sleeping={stats.sleepingCount}
          testId={`dashboard-site-card-${idText}-health`}
        />
      )}
    </div>
  );
};

export default SiteCard;
