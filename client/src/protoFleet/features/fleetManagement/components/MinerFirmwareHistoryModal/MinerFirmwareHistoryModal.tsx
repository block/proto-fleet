import { useNavigate } from "react-router-dom";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type MinerFirmwareHistoryEntry,
  RolloutDevicePhase,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type MinerFirmwareHistoryState, useMinerFirmwareHistory } from "@/protoFleet/api/useMinerFirmwareHistory";
import {
  isUnfinishedPhase,
  phaseLabels,
  phaseTone,
} from "@/protoFleet/features/settings/components/ReleaseChannels/rolloutStatus";
import StatusChip from "@/protoFleet/features/settings/components/ReleaseChannels/StatusChip";
import { linkedRolloutPath } from "@/protoFleet/features/settings/components/ReleaseChannels/useLinkedRollout";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type Column = "firmware" | "channel" | "outcome" | "attempts" | "verifiedAt" | "view";
const columns: Column[] = ["firmware", "channel", "outcome", "attempts", "verifiedAt", "view"];
const titles: ColTitles<Column> = {
  firmware: "Firmware",
  channel: "Release channel",
  outcome: "Miner outcome",
  attempts: "Attempts",
  verifiedAt: "Verified at",
  view: "",
};
function MinerOutcome({ entry }: { entry: MinerFirmwareHistoryEntry }) {
  const unfinished = isUnfinishedPhase(entry.phase);
  const canceled = entry.rolloutStatus === RolloutStatus.CANCELED && unfinished;
  const outcome = canceled
    ? { label: "Canceled", tone: "neutral" as const }
    : { label: phaseLabels[entry.phase] || "Unknown", tone: phaseTone(entry.phase) };
  return (
    <div className="flex flex-col items-start gap-1">
      <StatusChip {...outcome} />
      {entry.paused && entry.rolloutStatus === RolloutStatus.ACTIVE && unfinished ? (
        <span className="text-200 text-text-primary-50">Update paused</span>
      ) : null}
      {entry.lastError ? <span className="text-200 break-words text-text-primary-70">{entry.lastError}</span> : null}
      {entry.phase === RolloutDevicePhase.SKIPPED && entry.skipNote ? (
        <span className="text-200 break-words text-text-primary-70">{entry.skipNote}</span>
      ) : null}
      {entry.phase === RolloutDevicePhase.EXCLUDED ? (
        <span className="text-200 text-text-primary-50">Left the channel</span>
      ) : null}
      {canceled && entry.attempts > 0 ? (
        <span className="text-200 text-text-primary-50">Any update command already sent may still finish.</span>
      ) : null}
    </div>
  );
}

interface MinerFirmwareHistoryModalProps {
  deviceIdentifier: string;
  minerName?: string;
  onClose: () => void;
}

interface MinerFirmwareHistoryModalViewProps extends MinerFirmwareHistoryModalProps {
  history: MinerFirmwareHistoryState;
  onViewUpdate: (rolloutId: bigint) => void;
}

export function MinerFirmwareHistoryModalView({
  deviceIdentifier,
  minerName,
  history,
  onClose,
  onViewUpdate,
}: MinerFirmwareHistoryModalViewProps) {
  const busy = history.isLoading || history.isLoadingMore;
  const colConfig: ColConfig<MinerFirmwareHistoryEntry, bigint, Column> = {
    firmware: {
      width: "w-[140px]",
      allowWrap: true,
      component: (entry) => <span title={entry.firmwareChecksum}>{entry.firmwareVersion || "—"}</span>,
    },
    channel: { width: "w-[140px]", allowWrap: true, component: (entry) => entry.channelName },
    outcome: { width: "w-[210px]", allowWrap: true, component: (entry) => <MinerOutcome entry={entry} /> },
    attempts: { width: "w-[80px]", component: (entry) => entry.attempts.toLocaleString() },
    verifiedAt: {
      width: "w-[180px]",
      allowWrap: true,
      component: (entry) => {
        if (!entry.verifiedAt) return "—";
        const verifiedAt = timestampMs(entry.verifiedAt);
        return (
          <time dateTime={new Date(verifiedAt).toISOString()}>{formatTimestamp(Math.floor(verifiedAt / 1000))}</time>
        );
      },
    },
    view: {
      width: "w-[120px]",
      component: (entry) => (
        <Button text="View update" variant="secondary" size="compact" onClick={() => onViewUpdate(entry.rolloutId)} />
      ),
    },
  };

  return (
    <Modal
      open
      title="Firmware update history"
      description={minerName || deviceIdentifier}
      size="large"
      onDismiss={onClose}
      testId="miner-firmware-history-modal"
      buttons={[{ text: "Done", variant: "primary", onClick: onClose }]}
    >
      <div className="flex flex-col gap-4" aria-busy={busy}>
        <div className="flex items-center justify-between gap-4">
          <p className="text-200 text-text-primary-50">
            History includes only release channel updates, with attempt counts reset on retry and entries removed when
            their channel is deleted.
          </p>
          {history.canRead ? (
            <Button text="Refresh" variant="secondary" size="compact" disabled={busy} onClick={history.refresh} />
          ) : null}
        </div>
        {!history.canRead ? (
          <p role="status">Firmware update history is unavailable with your current permissions.</p>
        ) : (
          <>
            {history.error ? (
              <div role="alert">
                <Callout
                  intent="warning"
                  prefixIcon={<Alert />}
                  title="Couldn't load firmware update history"
                  subtitle={`${history.error}${history.hasLoaded ? " Showing the last loaded history." : ""}`}
                  buttonText="Retry"
                  buttonOnClick={history.retry}
                />
              </div>
            ) : null}
            <List<MinerFirmwareHistoryEntry, bigint, Column>
              activeCols={columns}
              colTitles={titles}
              colConfig={colConfig}
              items={history.entries}
              itemKey="rolloutId"
              itemName={{ singular: "update", plural: "updates" }}
              tableClassName="mb-0 w-full !table-fixed"
              applyColumnWidthsToCells
              stickyFirstColumn={false}
              emptyStateRow={
                <p role="status" className="py-10 text-center text-text-primary-70">
                  {history.isLoading
                    ? "Loading firmware update history…"
                    : history.hasLoaded
                      ? "No release channel updates for this miner yet."
                      : "Firmware update history unavailable."}
                </p>
              }
            />
            {history.hasMore ? (
              <div className="flex justify-center">
                <Button
                  text="Load older updates"
                  variant="secondary"
                  size="compact"
                  disabled={busy}
                  loading={history.isLoadingMore}
                  onClick={history.loadMore}
                />
              </div>
            ) : null}
          </>
        )}
      </div>
    </Modal>
  );
}

export default function MinerFirmwareHistoryModal(props: MinerFirmwareHistoryModalProps) {
  const history = useMinerFirmwareHistory(props.deviceIdentifier);
  const navigate = useNavigate();
  return (
    <MinerFirmwareHistoryModalView
      {...props}
      history={history}
      onViewUpdate={(rolloutId) => navigate(linkedRolloutPath(rolloutId))}
    />
  );
}
