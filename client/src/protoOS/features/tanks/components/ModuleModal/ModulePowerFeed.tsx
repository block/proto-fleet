import { modulePowerFeedTarget, useModulePowerFeed } from "./useModulePowerFeed";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type { ModuleNode, TankNode } from "@/protoOS/store/contract";
import Button from "@/shared/components/Button";
import PowerFeedCard from "@/shared/components/PowerFeedCard";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

// Fleet's miner proxy forwards none of the power-feed routes. Fleet's module
// page reads and controls the same feed through its own command path.
export const HOSTED_POWER_FEED_REASON =
  "Power feed readings need a direct connection to the controller. In Fleet, the module's page shows its power feed.";

/** The feed that supplies one module, read live from the controller on open and on refresh. */
export default function ModulePowerFeed({ module, tank }: { module: ModuleNode; tank: TankNode }) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const read = adapter?.getModulePowerFeed?.bind(adapter);
  const target = modulePowerFeedTarget(module, tank);
  const feed = useModulePowerFeed(read, target, !isFleetHosted);
  if (!read || !target) return null;

  if (isFleetHosted) return <p className="text-300 text-text-primary-70">{HOSTED_POWER_FEED_REASON}</p>;
  if (feed.data) {
    const readAt = formatTimestamp(feed.data.observedAt.getTime() / 1000, { includeSeconds: true });
    return (
      <PowerFeedCard
        feed={feed.data}
        actions={
          <>
            <span className="text-200 text-text-primary-70">Read {readAt}</span>
            <Button
              variant="secondary"
              size="compact"
              text={feed.refreshing ? "Refreshing…" : "Refresh"}
              ariaLabel="Refresh power feed"
              disabled={feed.refreshing}
              onClick={feed.refresh}
            />
          </>
        }
      />
    );
  }
  if (feed.error)
    return (
      <div className="flex flex-col items-start gap-2">
        <p role="alert">Could not load power feed: {feed.error}</p>
        <Button
          variant="secondary"
          text={feed.refreshing ? "Retrying…" : "Retry power feed"}
          ariaLabel="Retry power feed"
          disabled={feed.refreshing}
          onClick={feed.refresh}
        />
      </div>
    );
  return <p role="status">Loading power feed…</p>;
}
