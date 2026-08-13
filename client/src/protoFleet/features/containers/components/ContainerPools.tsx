import { Fragment, useState } from "react";
import PoolMonitorCard, { type ContainerPool } from "./PoolMonitorCard";
import Divider from "@/shared/components/Divider";
import DurationSelector from "@/shared/components/DurationSelector";

/**
 * Frame 3's time-range labels. The span matches protoOS `durations`
 * (1h/12h/24h/48h/5d); the design labels the 48h option "2d", so we pass a
 * frame-faithful label set rather than reusing the "48h" constant verbatim.
 */
const POOL_DURATIONS = ["1h", "12h", "24h", "2d", "5d"] as const;
export type PoolDuration = (typeof POOL_DURATIONS)[number];

export interface ContainerPoolsProps {
  pools: ContainerPool[];
  /** Selected time range; defaults to 24h when omitted. */
  duration?: PoolDuration;
  onSelectDuration?: (duration: PoolDuration) => void;
}

/**
 * Container Pools view (Frame 3): a read-only monitoring view of the pools the
 * container is mining to — distinct from the Settings → Mining Pools config
 * form. A 1h–5d time-range control sits above a stack of pools. The active pool
 * gets the design's prominent treatment (share stats + split bar + legend in an
 * elevated panel); standby pools render compactly and are separated by
 * dividers. The "Pools" nav label and the "Hashing" page-status chip belong to
 * the host page chrome, not this content pane. Presentational — the caller
 * supplies pre-derived pool telemetry and may control the selected range.
 * Shared at container and module scope.
 */
const ContainerPools = ({ pools, duration, onSelectDuration }: ContainerPoolsProps) => {
  const [uncontrolledDuration, setUncontrolledDuration] = useState<PoolDuration>("24h");
  const selectedDuration = duration ?? uncontrolledDuration;
  const handleSelectDuration = (nextDuration: PoolDuration) => {
    if (duration === undefined) setUncontrolledDuration(nextDuration);
    onSelectDuration?.(nextDuration);
  };

  return (
    <div className="flex flex-col gap-8 p-6 laptop:p-10" data-testid="container-pools">
      <DurationSelector<PoolDuration>
        duration={selectedDuration}
        durations={POOL_DURATIONS}
        onSelect={handleSelectDuration}
      />

      <div className="flex flex-col" data-testid="container-pools-list">
        {pools.map((pool, index) => (
          <Fragment key={pool.id}>
            {index > 0 ? <Divider className="my-8" /> : null}
            <PoolMonitorCard {...pool} prominent={pool.role === "active"} />
          </Fragment>
        ))}
      </div>
    </div>
  );
};

export default ContainerPools;
