import { Outlet, useLocation } from "react-router-dom";
import ContainerHomeToolbar from "./components/ContainerHomeToolbar";
import type { HomeTab } from "./types";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { getNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/utility";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDuration, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

// Home only needs the lightweight mining-status and plant-health responses.
// Topology is bootstrapped by App; no periodic board-map or board-history fetch.
const LIVE_LEVELS: TelemetryLevel[] = ["miner"];

export default function ContainerHomeLayout() {
  const { minerRoot } = useMinerHosting();
  const { pathname } = useLocation();
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
  };
  const tab =
    (Object.keys(links) as HomeTab[]).find((key) => links[key].replace(/\/$/, "") === pathname.replace(/\/$/, "")) ??
    "overview";
  const callout = getNoPoolsCalloutState(pools, pathname);
  return (
    <ErrorBoundary>
      <div className="flex min-w-0 flex-col gap-6 p-4 tablet:p-10">
        {callout.shouldShowNoPoolsCallout ? <NoPoolsCallout arePoolsConfigured={callout.arePoolsConfigured} /> : null}
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
