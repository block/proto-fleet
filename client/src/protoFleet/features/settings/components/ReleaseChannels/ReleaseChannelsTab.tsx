import { useEffect, useState } from "react";

import ChannelHistoryModal from "./ChannelHistoryModal";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import type { ReleaseChannel, Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const RELEASE_CHANNELS_DESCRIPTION =
  "Group miners into release channels and assign firmware per model. Assigned firmware is enforced: miners not on the assigned version are updated automatically, paced by the channel's update behavior.";

// Which surface the tab shows: the channels table, a channel's manage
// view, or the create form.
type View = { kind: "list" } | { kind: "manage"; channelId: bigint } | { kind: "create" };

interface ReleaseChannelsTabProps {
  // Shared with the active-updates monitor above the tabs, so one poll
  // feeds both.
  api: ReleaseChannelsApi;
  // Channel to open in the manage view on mount (e.g. from an update's
  // "Manage" action).
  initialManagedChannelId?: bigint | null;
  // History actions are handled by the active-updates monitor above the
  // tabs, which owns the update detail and the rollback confirmation.
  onViewRollout: (rollout: Rollout) => void;
  onRollbackRollout: (rollout: Rollout) => void;
}

const ReleaseChannelsTab = ({
  api,
  initialManagedChannelId = null,
  onViewRollout,
  onRollbackRollout,
}: ReleaseChannelsTabProps) => {
  const {
    channels,
    rollouts,
    minerNames,
    isLoading,
    createChannel,
    updateChannel,
    deleteChannel,
    previewScope,
    listChannelMiners,
    listRolloutDevices,
    applyFirmware,
  } = api;
  const { listFirmwareFiles } = useFirmwareApi();
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [view, setView] = useState<View>(() =>
    initialManagedChannelId !== null ? { kind: "manage", channelId: initialManagedChannelId } : { kind: "list" },
  );
  const [channelToDelete, setChannelToDelete] = useState<ReleaseChannel | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [historyChannelId, setHistoryChannelId] = useState<bigint | null>(null);

  useEffect(() => {
    listFirmwareFiles()
      .then(setFirmwareFiles)
      .catch((error) => {
        pushToast({ message: error?.message || "Failed to load firmware files", status: STATUSES.error });
      });
  }, [listFirmwareFiles]);

  const handleDelete = () => {
    if (!channelToDelete) return;
    setIsDeleting(true);
    deleteChannel(channelToDelete.id)
      .then(() => {
        pushToast({ message: `Deleted release channel ${channelToDelete.name}`, status: STATUSES.success });
        setChannelToDelete(null);
        setView({ kind: "list" });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't delete the release channel", status: STATUSES.error });
      })
      .finally(() => setIsDeleting(false));
  };

  // Resolved fresh on every poll so the manage view tracks live progress;
  // falls back to the table if the channel goes.
  const managedChannel = view.kind === "manage" ? channels.find((channel) => channel.id === view.channelId) : undefined;
  const showBack = view.kind === "create" || managedChannel !== undefined;
  // Resolved fresh on every poll so rollback eligibility tracks assignments.
  const historyChannel = historyChannelId !== null ? channels.find((c) => c.id === historyChannelId) : undefined;

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Release channels" description={RELEASE_CHANNELS_DESCRIPTION} />

      {showBack ? (
        <button
          type="button"
          data-testid="back-to-channels"
          className="flex cursor-pointer items-center gap-2 self-start text-200 text-text-primary-70 transition-colors hover:text-text-primary"
          onClick={() => setView({ kind: "list" })}
        >
          ← All release channels
        </button>
      ) : null}

      {view.kind === "create" ? (
        <ReleaseChannelManageView
          key="create"
          rollouts={rollouts}
          firmwareFiles={firmwareFiles}
          minerNames={minerNames}
          previewScope={previewScope}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onSave={async (draft) => {
            const created = await createChannel(draft);
            if (created) setView({ kind: "manage", channelId: created.id });
          }}
          onApply={async () => {}}
        />
      ) : managedChannel ? (
        <ReleaseChannelManageView
          key={managedChannel.id.toString()}
          channel={managedChannel}
          rollouts={rollouts}
          firmwareFiles={firmwareFiles}
          minerNames={minerNames}
          previewScope={previewScope}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onSave={async (draft) => {
            await updateChannel(managedChannel.id, draft);
          }}
          onDelete={setChannelToDelete}
          onShowHistory={(channel) => setHistoryChannelId(channel.id)}
          onApply={async (channelId, assignments) => {
            await applyFirmware(channelId, assignments);
          }}
        />
      ) : isLoading ? (
        <div className="text-center text-text-primary-50">Loading release channels...</div>
      ) : channels.length === 0 ? (
        <div className="flex flex-col gap-6">
          <div>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Create release channel"
              onClick={() => setView({ kind: "create" })}
              className="phone:w-full"
              testId="create-release-channel"
            />
          </div>
          <SettingsEmptyState
            title="No release channels"
            description="Create a release channel, choose which miners it applies to, and assign firmware per model to roll out updates."
          />
        </div>
      ) : (
        <ReleaseChannelsTable
          channels={channels}
          rollouts={rollouts}
          onCreate={() => setView({ kind: "create" })}
          onManage={(channel) => setView({ kind: "manage", channelId: channel.id })}
        />
      )}

      {historyChannel ? (
        <ChannelHistoryModal
          channel={historyChannel}
          rollouts={rollouts.filter((rollout) => rollout.channelId === historyChannel.id)}
          onView={(rollout) => {
            setHistoryChannelId(null);
            onViewRollout(rollout);
          }}
          onRollback={(rollout) => {
            setHistoryChannelId(null);
            onRollbackRollout(rollout);
          }}
          onClose={() => setHistoryChannelId(null)}
        />
      ) : null}

      <Dialog
        open={channelToDelete !== null}
        title="Delete release channel?"
        subtitle={`Miners in ${channelToDelete?.name ?? "this channel"} keep their current firmware, but it is no longer enforced for them and the channel's update history is removed.`}
        testId="delete-channel-dialog"
        onDismiss={() => {
          if (!isDeleting) setChannelToDelete(null);
        }}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setChannelToDelete(null),
            disabled: isDeleting,
          },
          {
            text: "Delete channel",
            variant: variants.danger,
            onClick: handleDelete,
            loading: isDeleting,
          },
        ]}
      />
    </div>
  );
};

export default ReleaseChannelsTab;
