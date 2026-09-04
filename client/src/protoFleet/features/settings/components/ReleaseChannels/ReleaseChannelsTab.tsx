import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import ChannelHistoryModal from "./ChannelHistoryModal";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import { useChannelHistory } from "./useChannelHistory";
import { useRefreshingRead } from "./useRefreshingRead";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useFleetStore, useIsAuthenticated, useSessionGeneration, useUsername } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const RELEASE_CHANNELS_DESCRIPTION =
  "Group miners into release channels and assign firmware per model. Assigned firmware is enforced: miners not on the assigned version are updated automatically, paced by the channel's update behavior.";

const FIRMWARE_REFRESH_INTERVAL_MS = 30_000;
const FIRMWARE_REQUEST_TIMEOUT_MS = 30_000;

// Which surface the tab shows: the channels table, a channel's manage
// view, or the create form.
type View = { kind: "list" } | { kind: "manage"; channelId: bigint } | { kind: "create" };

interface AcknowledgedWrites {
  authSessionIdentity: string;
  created: { id: bigint; name: string; snapshot: ChannelView[] } | null;
  deleted: { id: bigint; snapshot: ChannelView[] }[];
}

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
    hasLoaded,
    error,
    createChannel,
    updateChannel,
    deleteChannel,
    previewScope,
    listChannelMiners,
    listRolloutDevices,
    listChannelRollouts,
    applyFirmware,
  } = api;
  const { listFirmwareFiles } = useFirmwareApi();
  const isAuthenticated = useIsAuthenticated();
  const sessionGeneration = useSessionGeneration();
  const username = useUsername();
  const authSessionIdentity = JSON.stringify([username, sessionGeneration, isAuthenticated]);
  const firmwareCatalog = useRefreshingRead({
    read: listFirmwareFiles,
    enabled: isAuthenticated,
    errorMessage: "Failed to load firmware files",
    timeoutMs: FIRMWARE_REQUEST_TIMEOUT_MS,
    timeoutMessage: "The firmware file request timed out. Please retry.",
  });
  const { refresh: refreshFirmware } = firmwareCatalog;
  const firmwareFiles = firmwareCatalog.data ?? [];
  const [view, setView] = useState<View>(() =>
    initialManagedChannelId !== null ? { kind: "manage", channelId: initialManagedChannelId } : { kind: "list" },
  );
  const [expandedChannelIds, setExpandedChannelIds] = useState<bigint[]>([]);
  const [acknowledgedWrites, setAcknowledgedWrites] = useState<AcknowledgedWrites>({
    authSessionIdentity,
    created: null,
    deleted: [],
  });
  const currentWrites =
    isAuthenticated && acknowledgedWrites.authSessionIdentity === authSessionIdentity ? acknowledgedWrites : undefined;
  const latestSnapshotRef = useRef({ channels, error });
  latestSnapshotRef.current = { channels, error };
  const refreshAttemptRef = useRef<{ authSessionIdentity: string } | null>(null);
  const [refreshingSession, setRefreshingSession] = useState<string | null>(null);
  const isRefreshingChannels = refreshingSession === authSessionIdentity;
  const isCurrentSession = () => {
    const auth = useFleetStore.getState().auth;
    return auth.isAuthenticated && auth.username === username && auth.sessionGeneration === sessionGeneration;
  };
  const visibleChannels = useMemo(
    () => channels.filter((channel) => !currentWrites?.deleted.some(({ id }) => id === channel.id)),
    [channels, currentWrites],
  );
  if (acknowledgedWrites.authSessionIdentity !== authSessionIdentity) {
    setAcknowledgedWrites({ authSessionIdentity, created: null, deleted: [] });
  } else if (hasLoaded && error === null) {
    // Mutation helpers await a follow-up read. The snapshot at their completion
    // is the boundary: only a later successful read can confirm an absent ID.
    const created =
      acknowledgedWrites.created &&
      (channels.some(({ id }) => id === acknowledgedWrites.created?.id) ||
        channels !== acknowledgedWrites.created.snapshot)
        ? null
        : acknowledgedWrites.created;
    const deleted = acknowledgedWrites.deleted.filter(
      ({ id, snapshot }) => channels === snapshot || channels.some((channel) => channel.id === id),
    );
    if (created !== acknowledgedWrites.created || deleted.length !== acknowledgedWrites.deleted.length) {
      setAcknowledgedWrites({ ...acknowledgedWrites, created, deleted });
    }
  }
  const refreshChannels = async () => {
    if (refreshAttemptRef.current?.authSessionIdentity === authSessionIdentity || !isCurrentSession()) return;
    const attempt = { authSessionIdentity };
    refreshAttemptRef.current = attempt;
    setRefreshingSession(authSessionIdentity);
    try {
      await api.refresh();
    } catch {
      // The shared API exposes read failures on the page; keep the committed
      // write acknowledgement and allow another retry.
    } finally {
      if (refreshAttemptRef.current === attempt) {
        refreshAttemptRef.current = null;
        setRefreshingSession(null);
      }
    }
  };
  const [channelToDelete, setChannelToDelete] = useState<ChannelView | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const deletingRef = useRef(false);
  const writeInFlightRef = useRef(false);
  const [isWriting, setIsWriting] = useState(false);
  const tryAcquireWrite = useCallback(() => {
    if (writeInFlightRef.current) return false;
    writeInFlightRef.current = true;
    setIsWriting(true);
    return true;
  }, []);
  const releaseWrite = useCallback(() => {
    writeInFlightRef.current = false;
    setIsWriting(false);
  }, []);
  const writeLock = { isLocked: isWriting, tryAcquire: tryAcquireWrite, release: releaseWrite };
  const [historyChannelId, setHistoryChannelId] = useState<bigint | null>(null);

  useEffect(() => {
    if (!isAuthenticated) return;
    const interval = setInterval(refreshFirmware, FIRMWARE_REFRESH_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [authSessionIdentity, isAuthenticated, refreshFirmware]);

  const handleDelete = async () => {
    if (!channelToDelete || !tryAcquireWrite()) return;
    deletingRef.current = true;
    setIsDeleting(true);
    try {
      await deleteChannel(channelToDelete.id);
      if (!isCurrentSession()) return;
      const snapshot = latestSnapshotRef.current;
      if (snapshot.error !== null || snapshot.channels.some(({ id }) => id === channelToDelete.id)) {
        setAcknowledgedWrites((previous) => ({
          authSessionIdentity,
          created: previous.authSessionIdentity === authSessionIdentity ? previous.created : null,
          deleted: [
            ...(previous.authSessionIdentity === authSessionIdentity ? previous.deleted : []).filter(
              ({ id }) => id !== channelToDelete.id,
            ),
            { id: channelToDelete.id, snapshot: snapshot.channels },
          ],
        }));
      }
      pushToast({ message: `Deleted release channel ${channelToDelete.name}`, status: STATUSES.success });
      setChannelToDelete(null);
      setView({ kind: "list" });
    } catch (error) {
      if (!isCurrentSession()) return;
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Couldn't delete the release channel",
        status: STATUSES.error,
      });
    } finally {
      deletingRef.current = false;
      setIsDeleting(false);
      releaseWrite();
    }
  };
  const dismissDelete = () => {
    if (!deletingRef.current) setChannelToDelete(null);
  };

  // Resolved fresh on every poll so the manage view tracks live progress;
  // falls back to the table if the channel goes.
  const managedChannel =
    view.kind === "manage" ? visibleChannels.find((channel) => channel.id === view.channelId) : undefined;
  const pendingCreate = currentWrites?.created;
  const hasPendingWrites = !!pendingCreate || !!currentWrites?.deleted.length;
  const awaitingCreatedChannel =
    view.kind === "manage" && !managedChannel && (pendingCreate?.id === view.channelId || error !== null);
  const showBack = view.kind === "create" || managedChannel !== undefined || awaitingCreatedChannel;
  const openCreate = () => {
    if (!writeInFlightRef.current) {
      setView(pendingCreate ? { kind: "manage", channelId: pendingCreate.id } : { kind: "create" });
    }
  };
  // Resolved fresh on every poll so rollback eligibility tracks assignments.
  const historyChannel = historyChannelId !== null ? channels.find((c) => c.id === historyChannelId) : undefined;
  const historyChannelIds =
    !hasLoaded || view.kind === "create" || awaitingCreatedChannel
      ? []
      : managedChannel
        ? [managedChannel.id]
        : expandedChannelIds.filter((id) => visibleChannels.some((channel) => channel.id === id));
  const history = useChannelHistory({ channelIds: historyChannelIds, rollouts, listChannelRollouts });

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Release channels" description={RELEASE_CHANNELS_DESCRIPTION} />

      {historyChannelIds.map((id) => {
        const state = history.states.get(id);
        if (state?.status !== "error") return null;
        const name = visibleChannels.find((channel) => channel.id === id)?.name ?? "this channel";
        return (
          <Callout
            key={id.toString()}
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={`Couldn't load update history for ${name}`}
            subtitle={state.error}
            buttonText="Retry update history"
            buttonOnClick={() => history.retry(id)}
            testId={`channel-history-error-${id}`}
          />
        );
      })}

      {hasPendingWrites ? (
        <Callout
          intent={intents.information}
          prefixIcon={<Alert />}
          title={pendingCreate ? `Created release channel ${pendingCreate.name}` : "Release channel deletion saved"}
          subtitle={
            pendingCreate
              ? "Refresh the channel list to load the saved channel before creating another."
              : "Deleted channels stay hidden while the updated channel list is unavailable."
          }
          buttonText={isRefreshingChannels ? "Refreshing..." : "Refresh channel list"}
          buttonOnClick={refreshChannels}
          testId="channel-write-pending"
        />
      ) : null}

      {firmwareCatalog.error ? (
        <div role="alert" aria-busy={firmwareCatalog.isLoading}>
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={firmwareCatalog.data === null ? "Couldn't load firmware files" : "Firmware files may be out of date"}
            subtitle={
              firmwareCatalog.data === null
                ? firmwareCatalog.error
                : `${firmwareCatalog.error} Showing the last loaded firmware files.`
            }
            buttonText={firmwareCatalog.isLoading ? "Retrying..." : "Retry"}
            buttonOnClick={refreshFirmware}
            testId="release-channel-firmware-load-error"
          />
        </div>
      ) : isAuthenticated && !firmwareCatalog.data ? (
        <p className="text-200 text-text-primary-70">Loading firmware files...</p>
      ) : null}

      {showBack ? (
        <button
          type="button"
          data-testid="back-to-channels"
          className="flex cursor-pointer items-center gap-2 self-start text-200 text-text-primary-70 transition-colors hover:text-text-primary"
          disabled={isWriting}
          onClick={() => {
            if (!writeInFlightRef.current) setView({ kind: "list" });
          }}
        >
          ← All release channels
        </button>
      ) : null}

      {!hasLoaded ? (
        isLoading ? (
          <div className="text-center text-text-primary-50">Loading release channels...</div>
        ) : null
      ) : view.kind === "create" ? (
        <ReleaseChannelManageView
          key="create"
          writeLock={writeLock}
          rollouts={rollouts}
          firmwareFiles={firmwareFiles}
          minerNames={minerNames}
          previewScope={previewScope}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onSave={async (draft) => {
            const created = await createChannel(draft);
            if (!created || !isCurrentSession()) return;
            const snapshot = latestSnapshotRef.current.channels;
            setAcknowledgedWrites((previous) => ({
              authSessionIdentity,
              created: snapshot.some(({ id }) => id === created.id)
                ? null
                : { id: created.id, name: created.name, snapshot },
              deleted: previous.authSessionIdentity === authSessionIdentity ? previous.deleted : [],
            }));
            setView({ kind: "manage", channelId: created.id });
          }}
          onApply={async () => {}}
        />
      ) : managedChannel ? (
        <ReleaseChannelManageView
          key={managedChannel.id.toString()}
          channel={managedChannel}
          writeLock={writeLock}
          hasRefreshError={error !== null}
          rollouts={history.rollouts}
          historyState={history.states.get(managedChannel.id) ?? { status: "loading" }}
          firmwareFiles={firmwareFiles}
          minerNames={minerNames}
          previewScope={previewScope}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onSave={async (draft) => {
            await updateChannel(managedChannel.id, draft);
          }}
          onDelete={(channel) => {
            if (!writeInFlightRef.current) setChannelToDelete(channel);
          }}
          onShowHistory={(channel) => setHistoryChannelId(channel.id)}
          onApply={applyFirmware}
        />
      ) : awaitingCreatedChannel ? (
        <p className="text-text-primary-70">Channel details will appear after a successful refresh.</p>
      ) : pendingCreate && visibleChannels.length === 0 ? (
        <p className="text-text-primary-70">Channel details will appear after a successful refresh.</p>
      ) : visibleChannels.length === 0 ? (
        <div className="flex flex-col gap-6">
          <div>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Create release channel"
              disabled={isWriting}
              onClick={openCreate}
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
          channels={visibleChannels}
          rollouts={history.rollouts}
          historyStates={history.states}
          onExpandedChannelIdsChange={setExpandedChannelIds}
          onCreate={openCreate}
          onManage={(channel) => {
            if (!writeInFlightRef.current) setView({ kind: "manage", channelId: channel.id });
          }}
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
        onDismiss={dismissDelete}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: dismissDelete,
            disabled: isDeleting,
          },
          {
            text: "Delete channel",
            variant: variants.danger,
            onClick: handleDelete,
            disabled: isWriting,
            loading: isDeleting,
          },
        ]}
      />
    </div>
  );
};

export default ReleaseChannelsTab;
