import { useEffect, useMemo, useState } from "react";
import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { MeasurementType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { siteFilterFromActive, useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { TemperaturePanel } from "@/protoFleet/features/dashboard/components/TemperaturePanel";
import { UptimePanel } from "@/protoFleet/features/dashboard/components/UptimePanel";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useStickyState } from "@/shared/hooks/useStickyState";
import { buildVersionInfo } from "@/shared/utils/version";

// Constants for telemetry options - stable references to prevent unnecessary re-renders
const ALL_DEVICES: string[] = [];
const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const Dashboard = () => {
  const { devicePaired, statusLoaded } = useOnboardedStatus();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const currentYear = new Date().getFullYear();
  const { refs } = useStickyState();

  // Load the org's sites so useActiveSite can validate the route scope: a
  // stale/deleted site id (route or persisted activeSite) falls back to
  // all-sites instead of resolving zero devices into an empty dashboard.
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [siteValidationSettled, setSiteValidationSettled] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: setSites,
      onFinally: () => {
        if (!controller.signal.aborted) {
          setSiteValidationSettled(true);
        }
      },
    });
    return () => controller.abort();
  }, [listSites]);

  // Active site comes from the route path (`/`, `/:site`, `/unassigned`),
  // validated against knownSiteIds. All-sites yields an empty filter, so
  // `/dashboard` stays org-wide.
  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  const siteFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);
  const routeScope = useRouteSiteScope();
  const scopedRouteReady = routeScope?.kind !== "site" || knownSiteIds !== undefined || siteValidationSettled;

  // Fleet counts — polled for fresh minerStateCounts, scoped to the active site
  const {
    totalMiners,
    stateCounts,
    hasLoaded: countsLoaded,
  } = useFleetCounts({
    enabled: scopedRouteReady,
    pollIntervalMs: POLL_INTERVAL_MS,
    siteIds: siteFilter.siteIds,
    includeUnassigned: siteFilter.includeUnassigned,
  });

  // Component errors — polled, local state (no store), scoped to the active site
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors({
    enabled: scopedRouteReady,
    pollIntervalMs: POLL_INTERVAL_MS,
    siteIds: siteFilter.siteIds,
    includeUnassigned: siteFilter.includeUnassigned,
  });

  // Combined telemetry — polled, replaces data each cycle (no streaming merge)
  const telemetryOptions = useMemo(
    () => ({
      deviceIds: ALL_DEVICES,
      measurementTypes: ALL_MEASUREMENT_TYPES,
      duration,
      enabled: scopedRouteReady,
      pollIntervalMs: POLL_INTERVAL_MS,
      siteIds: siteFilter.siteIds,
      includeUnassigned: siteFilter.includeUnassigned,
    }),
    [duration, scopedRouteReady, siteFilter],
  );

  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);

  // Extract metrics for panels — filter by measurement type
  const allMetrics = telemetryData?.metrics;
  const hashrateMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.HASHRATE),
    [allMetrics],
  );
  const powerMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.POWER),
    [allMetrics],
  );
  const efficiencyMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.EFFICIENCY),
    [allMetrics],
  );
  const temperatureStatusCounts = telemetryData?.temperatureStatusCounts;
  const uptimeStatusCounts = telemetryData?.uptimeStatusCounts;

  if (!statusLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="h-full">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup className="p-6 laptop:p-10" />

          {/* Overview Section */}
          <section className="p-6 laptop:p-10">
            <SectionHeading heading="Overview" />
            <div className="mt-6 flex flex-col gap-1">
              <FleetHealth
                fleetSize={countsLoaded ? totalMiners : undefined}
                healthyMiners={countsLoaded ? (stateCounts?.hashingCount ?? null) : undefined}
                needsAttentionMiners={countsLoaded ? (stateCounts?.brokenCount ?? null) : undefined}
                offlineMiners={countsLoaded ? (stateCounts?.offlineCount ?? null) : undefined}
                sleepingMiners={countsLoaded ? (stateCounts?.sleepingCount ?? null) : undefined}
              />
              <FleetErrors
                controlBoardErrors={controlBoardErrors}
                fanErrors={fanErrors}
                hashboardErrors={hashboardErrors}
                psuErrors={psuErrors}
              />
            </div>
          </section>

          {/* Performance Section */}
          <section className="pb-6">
            <div ref={refs.vertical.start} />
            <div className="sticky top-0 z-2 bg-surface-5 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10 dark:bg-surface-base">
              <SectionHeading heading="Performance">
                <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
              </SectionHeading>
            </div>

            <div className="flex flex-col gap-1 px-6 laptop:px-10">
              <HashratePanel duration={duration} metrics={hashrateMetrics} />
              <UptimePanel duration={duration} uptimeStatusCounts={uptimeStatusCounts} />
              <TemperaturePanel duration={duration} temperatureStatusCounts={temperatureStatusCounts} />

              <div className="grid grid-cols-1 gap-1 laptop:grid-cols-2">
                <PowerPanel duration={duration} metrics={powerMetrics} totalMiners={totalMiners} />
                <EfficiencyPanel duration={duration} metrics={efficiencyMetrics} totalMiners={totalMiners} />
              </div>
            </div>

            <p className="px-6 pt-6 text-300 text-text-primary laptop:px-10">
              Some devices do not make all data available to Proto Fleet.
            </p>
            {/* eslint-disable-next-line react-hooks/refs -- ref object from useStickyState is passed to <div ref>; React writes .current during commit, not read during render */}
            <div ref={refs.vertical.end} />
          </section>

          {/* Privacy Policy */}
          <footer className="px-5 pt-20 pb-6 text-300 laptop:px-10">
            <p className="text-text-primary">
              Powerful mining tools. Built for decentralization.{" "}
              <span className="text-text-primary-50">
                Proto Fleet {buildVersionInfo.version} © {currentYear} Block, Inc.{" "}
                <a
                  href="https://proto.xyz/privacy-policy"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:underline"
                >
                  Privacy Notice
                </a>
              </span>
            </p>
          </footer>
        </div>
      ) : (
        <MinersPage />
      )}
    </div>
  );
};

export default Dashboard;
