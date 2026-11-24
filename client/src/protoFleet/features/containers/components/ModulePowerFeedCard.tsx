import { type ComponentProps, type ReactNode, useState } from "react";
import { useModulePowerFeed } from "../hooks/useModulePowerFeed";
import { modulePowerFeedControl, modulePowerFeedView } from "../modulePowerFeed";
import PowerFeedActionDialog from "../PowerFeedActionDialog";
import { Play, Power, Reboot } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import PowerFeedCard, { type PowerFeedAction, powerFeedActionLabels } from "@/shared/components/PowerFeedCard";
import RowActionsMenu from "@/shared/components/RowActionsMenu";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const actionIcons: Record<PowerFeedAction, ReactNode> = {
  turn_on: <Play />,
  turn_off: <Power />,
  cycle: <Reboot />,
  clear_override: <Power />,
};

type Confirmation = Omit<ComponentProps<typeof PowerFeedActionDialog>, "onClose" | "onSettled">;

function ModulePowerFeedBody({
  feed,
  onAction,
}: {
  feed: ReturnType<typeof useModulePowerFeed>;
  onAction: (confirmation: Confirmation) => void;
}) {
  const data = feed.data;
  if (!data) {
    if (feed.error)
      return (
        <div>
          <p role="alert">Could not load power feed: {feed.error}</p>
          <Button variant="secondary" text="Retry power feed" onClick={feed.refresh} />
        </div>
      );
    return feed.loading ? <p role="status">Loading power feed…</p> : null;
  }
  const control = modulePowerFeedControl(data);
  // With no action to offer, the menu still says why.
  const menu =
    control && (control.actions.length || control.unavailableReason) ? (
      <RowActionsMenu
        ariaLabel="Power feed actions"
        testIdPrefix="power-feed-actions"
        actions={[
          ...control.actions.map((action) => ({
            label: powerFeedActionLabels[action],
            icon: actionIcons[action],
            disabled: !!control.unavailableReason,
            onClick: () =>
              onAction({
                moduleId: data.moduleId,
                deviceIdentifier: data.deviceIdentifier,
                feedId: control.feedId,
                containerName: data.containerName,
                tank: data.tank,
                module: data.module,
                action,
                cycleHoldSeconds: control.cycleHoldSeconds,
              }),
          })),
          // One reason covers every action, so it gets its own row.
          {
            label: control.unavailableReason ?? "",
            hidden: !control.unavailableReason,
            disabled: true,
            onClick: () => undefined,
          },
        ]}
      />
    ) : null;
  // Read on demand, so say when the reading was taken.
  const readAt = data.observedAt
    ? formatTimestamp(Number(data.observedAt.seconds), { includeSeconds: true })
    : undefined;
  return (
    <PowerFeedCard
      feed={modulePowerFeedView(data)}
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
          {menu}
        </>
      }
    />
  );
}

export default function ModulePowerFeedCard({ moduleId, enabled }: { moduleId: bigint; enabled: boolean }) {
  const feed = useModulePowerFeed(moduleId, enabled);
  // Pinned at click time: later refreshes, or a failed one, never change or
  // unmount what the operator confirmed while its outcome is tracked.
  const [confirmation, setConfirmation] = useState<Confirmation>();
  return (
    <>
      <ModulePowerFeedBody feed={feed} onAction={setConfirmation} />
      {confirmation ? (
        <PowerFeedActionDialog {...confirmation} onClose={() => setConfirmation(undefined)} onSettled={feed.refresh} />
      ) : null}
    </>
  );
}
