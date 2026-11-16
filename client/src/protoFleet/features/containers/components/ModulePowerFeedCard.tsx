import { useModulePowerFeed } from "../hooks/useModulePowerFeed";
import { modulePowerFeedView } from "../modulePowerFeed";
import Button from "@/shared/components/Button";
import PowerFeedCard from "@/shared/components/PowerFeedCard";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

export default function ModulePowerFeedCard({ moduleId, enabled }: { moduleId: bigint; enabled: boolean }) {
  const feed = useModulePowerFeed(moduleId, enabled);
  if (feed.data) {
    // Read on demand, so say when the reading was taken.
    const readAt = feed.data.observedAt
      ? formatTimestamp(Number(feed.data.observedAt.seconds), { includeSeconds: true })
      : undefined;
    return (
      <PowerFeedCard
        feed={modulePowerFeedView(feed.data)}
        actions={
          <>
            {readAt ? <span className="text-200 text-text-primary-70">Read {readAt}</span> : null}
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
      <div>
        <p role="alert">Could not load power feed: {feed.error}</p>
        <Button variant="secondary" text="Retry power feed" onClick={feed.refresh} />
      </div>
    );
  return feed.loading ? <p role="status">Loading power feed…</p> : null;
}
