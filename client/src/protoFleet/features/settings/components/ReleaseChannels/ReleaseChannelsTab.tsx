import { useCallback, useEffect, useRef, useState } from "react";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
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

interface ReleaseChannelsTabProps {
  // Shared with the active-updates monitor above the tabs, so one poll
  // feeds both.
  api: ReleaseChannelsApi;
  // Channel to open in the manage view on mount (e.g. from an update's
  // "Manage" action).
  initialManagedChannelId?: bigint | null;
}

const ReleaseChannelsTab = ({ api, initialManagedChannelId = null }: ReleaseChannelsTabProps) => {
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
    applyFirmware,
  } = api;
  const { listFirmwareFiles } = useFirmwareApi();
  const isAuthenticated = useIsAuthenticated();
  const sessionGeneration = useSessionGeneration();
  const username = useUsername();
  const authSessionIdentity = JSON.stringify([username, sessionGeneration]);
  const [firmwareCatalog, setFirmwareCatalog] = useState({
    authSessionIdentity,
    files: null as FirmwareFileInfo[] | null,
    error: null as string | null,
    isLoading: true,
  });
  const refreshFirmwareRef = useRef<(() => void) | null>(null);
  const currentCatalog =
    isAuthenticated && firmwareCatalog.authSessionIdentity === authSessionIdentity ? firmwareCatalog : undefined;
  const firmwareFiles = currentCatalog?.files ?? [];
  const [view, setView] = useState<View>(() =>
    initialManagedChannelId !== null ? { kind: "manage", channelId: initialManagedChannelId } : { kind: "list" },
  );
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

  useEffect(() => {
    if (!isAuthenticated) return;
    let canceled = false;
    let controller: AbortController | null = null;
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const isCurrentSession = () => {
      const auth = useFleetStore.getState().auth;
      return (
        !canceled && auth.isAuthenticated && auth.sessionGeneration === sessionGeneration && auth.username === username
      );
    };
    const load = () => {
      if (controller || !isCurrentSession()) return false;
      controller = new AbortController();
      const requestController = controller;
      timeout = setTimeout(
        () => requestController.abort(new Error("The firmware file request timed out. Please retry.")),
        FIRMWARE_REQUEST_TIMEOUT_MS,
      );
      listFirmwareFiles(requestController.signal)
        .then((files) => {
          if (isCurrentSession()) setFirmwareCatalog({ authSessionIdentity, files, error: null, isLoading: false });
        })
        .catch((error: unknown) => {
          if (!isCurrentSession()) return;
          setFirmwareCatalog((previous) => ({
            authSessionIdentity,
            files: previous.authSessionIdentity === authSessionIdentity ? previous.files : null,
            error: error instanceof Error && error.message ? error.message : "Failed to load firmware files",
            isLoading: false,
          }));
        })
        .finally(() => {
          clearTimeout(timeout);
          controller = null;
        });
      return true;
    };
    const refresh = () => {
      if (load()) {
        setFirmwareCatalog((previous) => ({
          authSessionIdentity,
          files: previous.authSessionIdentity === authSessionIdentity ? previous.files : null,
          error: previous.authSessionIdentity === authSessionIdentity ? previous.error : null,
          isLoading: true,
        }));
      }
    };
    refreshFirmwareRef.current = refresh;
    load();
    const interval = setInterval(refresh, FIRMWARE_REFRESH_INTERVAL_MS);
    return () => {
      canceled = true;
      clearInterval(interval);
      clearTimeout(timeout);
      controller?.abort();
      refreshFirmwareRef.current = null;
    };
  }, [authSessionIdentity, isAuthenticated, listFirmwareFiles, sessionGeneration, username]);

  const handleDelete = async () => {
    if (!channelToDelete || !tryAcquireWrite()) return;
    deletingRef.current = true;
    setIsDeleting(true);
    try {
      await deleteChannel(channelToDelete.id);
      pushToast({ message: `Deleted release channel ${channelToDelete.name}`, status: STATUSES.success });
      setChannelToDelete(null);
      setView({ kind: "list" });
    } catch (error) {
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
  const managedChannel = view.kind === "manage" ? channels.find((channel) => channel.id === view.channelId) : undefined;
  const awaitingCreatedChannel = view.kind === "manage" && !managedChannel && error !== null;
  const showBack = view.kind === "create" || managedChannel !== undefined || awaitingCreatedChannel;

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Release channels" description={RELEASE_CHANNELS_DESCRIPTION} />

      {currentCatalog?.error ? (
        <div role="alert" aria-busy={currentCatalog.isLoading}>
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={currentCatalog.files === null ? "Couldn't load firmware files" : "Firmware files may be out of date"}
            subtitle={
              currentCatalog.files === null
                ? currentCatalog.error
                : `${currentCatalog.error} Showing the last loaded firmware files.`
            }
            buttonText={currentCatalog.isLoading ? "Retrying..." : "Retry"}
            buttonOnClick={() => refreshFirmwareRef.current?.()}
            testId="release-channel-firmware-load-error"
          />
        </div>
      ) : isAuthenticated && !currentCatalog?.files ? (
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
            if (created) setView({ kind: "manage", channelId: created.id });
          }}
          onApply={async () => {}}
        />
      ) : managedChannel ? (
        <ReleaseChannelManageView
          key={managedChannel.id.toString()}
          channel={managedChannel}
          writeLock={writeLock}
          hasRefreshError={error !== null}
          rollouts={rollouts}
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
          onApply={applyFirmware}
        />
      ) : awaitingCreatedChannel ? (
        <p className="text-text-primary-70">Channel details will appear after a successful refresh.</p>
      ) : channels.length === 0 ? (
        <div className="flex flex-col gap-6">
          <div>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Create release channel"
              disabled={isWriting}
              onClick={() => {
                if (!writeInFlightRef.current) setView({ kind: "create" });
              }}
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
          onCreate={() => {
            if (!writeInFlightRef.current) setView({ kind: "create" });
          }}
          onManage={(channel) => {
            if (!writeInFlightRef.current) setView({ kind: "manage", channelId: channel.id });
          }}
        />
      )}

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
