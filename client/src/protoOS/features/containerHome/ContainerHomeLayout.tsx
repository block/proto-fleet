import { Outlet, useLocation } from "react-router-dom";
import ContainerHomeToolbar from "./components/ContainerHomeToolbar";
import type { HomeTab } from "./types";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { useNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/useNoPoolsCalloutState";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ContainerMappingSetup from "@/protoOS/features/feedMapping/MappingSetup";
import { useDuration, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

// One bulk container-map request adds live module power/temperature to the
// mining-status and plant-health poll; no per-board requests or histories.
const LIVE_LEVELS: TelemetryLevel[] = ["miner", "hashboard"];

export default function ContainerHomeLayout() {
  const { minerRoot } = useMinerHosting();
  const { pathname, search } = useLocation();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const pools = usePoolsInfo();
  const live = useTelemetry({ level: LIVE_LEVELS });
  const links: Record<HomeTab, string> = {
    overview: `${minerRoot}/`,
    hashrate: `${minerRoot}/hashrate`,
    efficiency: `${minerRoot}/efficiency`,
    power: `${minerRoot}/power-usage`,
    temperature: `${minerRoot}/temperature`,
    system: `${minerRoot}/?view=system`,
  };
  const pathTab =
    (Object.keys(links) as HomeTab[]).find((key) => links[key].replace(/\/$/, "") === pathname.replace(/\/$/, "")) ??
    "overview";
  const tab = pathTab === "overview" && new URLSearchParams(search).get("view") === "system" ? "system" : pathTab;
  const callout = useNoPoolsCalloutState(pools, pathname);
  return (
    <ErrorBoundary>
      <div className="flex min-w-0 flex-col gap-6 p-4 tablet:p-10">
        {callout.shouldShowNoPoolsCallout ? <NoPoolsCallout arePoolsConfigured={callout.arePoolsConfigured} /> : null}
        <ContainerMappingSetup />
        <ContainerHomeToolbar tab={tab} links={links} duration={duration} onDurationChange={setDuration} />
        {live.error ? (
          <p role="alert" className="text-300 text-text-critical">
            Could not refresh live readings. Displayed values may be out of date.
          </p>
        ) : null}
        <Outlet />
      </div>
    </ErrorBoundary>
  );
}
