import { type ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { createPortal } from "react-dom";
import { HASHBOARD_FIRMWARE_LABEL, PSU_FIRMWARE_LABEL } from "@/protoFleet/api/firmwareComponent";
import { useContainers } from "@/protoFleet/api/useContainers";
import {
  type FirmwareFileInfo,
  type FirmwareMetadataInput,
  isCT1FirmwareTarget,
  PSU_FIRMWARE_COMPONENT,
  useFirmwareApi,
} from "@/protoFleet/api/useFirmwareApi";
import { useFrigatePackages } from "@/protoFleet/api/useFrigatePackages";
import { type ReleaseChannelsApi, useReleaseChannels } from "@/protoFleet/api/useReleaseChannels";
import DeleteAllFirmwareDialog from "@/protoFleet/features/settings/components/DeleteAllFirmwareDialog";
import DeleteFirmwareDialog from "@/protoFleet/features/settings/components/DeleteFirmwareDialog";
import EditFirmwareMetadataDialog from "@/protoFleet/features/settings/components/EditFirmwareMetadataDialog";
import FirmwarePageLayout from "@/protoFleet/features/settings/components/FirmwarePageLayout";
import {
  FirmwareArtifactSection,
  FirmwareDeviceSection,
} from "@/protoFleet/features/settings/components/FirmwareSections";
import FirmwareUploadDialog from "@/protoFleet/features/settings/components/FirmwareUploadDialog";
import FirmwareUsageBadge from "@/protoFleet/features/settings/components/FirmwareUsageBadge";
import FrigatePackages from "@/protoFleet/features/settings/components/FrigatePackages";
import ActiveUpdatesMonitor, {
  type MonitorRequest,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ActiveUpdatesMonitor";
import ReleaseChannelsTab from "@/protoFleet/features/settings/components/ReleaseChannels/ReleaseChannelsTab";
import {
  LINKED_ROLLOUT_PARAM,
  useLinkedRollout,
} from "@/protoFleet/features/settings/components/ReleaseChannels/useLinkedRollout";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useHasPermission } from "@/protoFleet/store";
import { Alert, ChevronDown, Edit, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

type FirmwareFileData = {
  component?: string;
  hashboardBuild?: string;
  id: string;
  filename: string;
  targetManufacturer: string;
  targetModel: string;
  firmwareVersion: string;
  checksum: string;
  size: number;
  uploadedAt: number;
};

type FirmwareColumns = "filename" | "target" | "firmwareVersion" | "uploadedAt" | "size";

const colTitles: ColTitles<FirmwareColumns> = {
  filename: "File name",
  target: "Target",
  firmwareVersion: "Version",
  uploadedAt: "Uploaded",
  size: "Size",
};

const ExpandableFilename = ({ filename }: { filename: string }) => {
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const measurementRef = useRef<HTMLSpanElement>(null);
  const actionLabel = `${expanded ? "Hide" : "Show"} full file name: ${filename}`;

  useLayoutEffect(() => {
    const container = containerRef.current;
    const measurement = measurementRef.current;
    if (!container || !measurement) return;

    const updateOverflow = (): void => {
      const nextOverflows = measurement.scrollWidth > container.clientWidth;
      setOverflows(nextOverflows);
      if (!nextOverflows) setExpanded(false);
    };

    updateOverflow();
    if (typeof ResizeObserver === "undefined") {
      // Without ResizeObserver, approximate container resizes with window resizes.
      window.addEventListener("resize", updateOverflow);
      return () => window.removeEventListener("resize", updateOverflow);
    }

    const observer = new ResizeObserver(updateOverflow);
    observer.observe(container);
    return () => observer.disconnect();
  }, [filename]);

  return (
    <div ref={containerRef} className="relative w-full text-emphasis-300">
      <span
        ref={measurementRef}
        aria-hidden
        data-filename={filename}
        className="pointer-events-none invisible absolute whitespace-nowrap before:content-[attr(data-filename)]"
      />
      {overflows ? (
        <button
          type="button"
          aria-expanded={expanded}
          aria-label={actionLabel}
          title={actionLabel}
          className="flex w-full cursor-pointer items-start gap-1.5 text-left"
          onClick={() => setExpanded((current) => !current)}
        >
          <span className={clsx("min-w-0", expanded ? "break-all whitespace-normal" : "truncate")}>{filename}</span>
          <ChevronDown width="w-3" className={clsx("mt-1 shrink-0 transition-transform", expanded && "rotate-180")} />
        </button>
      ) : (
        <span className="block truncate">{filename}</span>
      )}
    </div>
  );
};

const colConfig: ColConfig<FirmwareFileData, string, FirmwareColumns> = {
  filename: {
    component: (file) => <ExpandableFilename filename={file.filename} />,
    width: "w-96",
    allowWrap: true,
  },
  target: {
    component: (file) => <span>{`${file.targetManufacturer} ${file.targetModel}`.trim() || "Unknown"}</span>,
    width: "w-48",
  },
  uploadedAt: {
    component: (file) => <span>{formatTimestamp(file.uploadedAt)}</span>,
    width: "w-48",
  },
  size: {
    component: (file) => <span>{formatFileSize(file.size)}</span>,
    width: "w-40",
  },
};

const activeCols: FirmwareColumns[] = ["filename", "target", "firmwareVersion", "uploadedAt", "size"];

function toFileData(info: FirmwareFileInfo): FirmwareFileData {
  return {
    id: info.id,
    filename: info.filename,
    targetManufacturer: info.target_manufacturer,
    targetModel: info.target_model,
    firmwareVersion: info.firmware_version ?? "",
    hashboardBuild: info.hashboard_build,
    component: info.component,
    checksum: info.sha256 ?? "",
    size: info.size,
    uploadedAt: isoToEpochSeconds(info.uploaded_at),
  };
}

// Container firmware is listed under Proto Containers, apart from miner firmware.
const isContainerFirmware = (file: FirmwareFileData): boolean =>
  isCT1FirmwareTarget({ targetManufacturer: file.targetManufacturer, targetModel: file.targetModel });

// The kinds of firmware file a Proto Container takes, each listed under its
// own heading in this order. A file belongs to the first kind that accepts it.
const CONTAINER_FIRMWARE_KINDS: { key: string; title: string; accepts: (file: FirmwareFileData) => boolean }[] = [
  // Every container file that is not a PSU capsule is a hashboard archive.
  { key: "hashboard", title: HASHBOARD_FIRMWARE_LABEL, accepts: (file) => file.component !== PSU_FIRMWARE_COMPONENT },
  { key: "psu", title: PSU_FIRMWARE_LABEL, accepts: (file) => file.component === PSU_FIRMWARE_COMPONENT },
];

/** Container files grouped by kind; a kind with no file is left out. */
const containerFirmwareSections = (files: FirmwareFileData[]) =>
  CONTAINER_FIRMWARE_KINDS.map((kind) => ({
    kind,
    files: files.filter((file) => CONTAINER_FIRMWARE_KINDS.find((candidate) => candidate.accepts(file)) === kind),
  })).filter((section) => section.files.length > 0);

/**
 * Whether a Proto Container is paired, as far as this caller is concerned.
 * `hasContainers` is undefined while that is unknown: still loading, not
 * permitted to list containers, or a check failed. A failed refresh keeps the
 * last answer.
 *
 * Listing containers needs read access at their site; updating controller
 * software does not. So before an empty list hides the section from a caller
 * who may update controller software, the server is asked whether a container
 * is paired where that caller may update it.
 */
const usePairedContainers = (
  canUpdateControllerSoftware: boolean,
): { hasContainers: boolean | undefined; loading: boolean } => {
  const canReadSites = useHasPermission("site:read");
  const canReadFleet = useHasPermission("fleet:read");
  const canReadMiners = useHasPermission("miner:read");
  // ListContainers returns a container only to callers holding all three.
  const canRead = canReadSites && canReadFleet && canReadMiners;
  const catalog = useContainers([], false, canRead);
  const { hasUpdatableContainer } = useFrigatePackages();
  const [hasContainers, setHasContainers] = useState<boolean>();
  const loaded = canRead && !catalog.loading && catalog.error === undefined;
  const paired = catalog.containers.length > 0;
  if (loaded && paired !== hasContainers) setHasContainers(paired);
  const listed = canRead ? hasContainers : undefined;

  const askServer = canUpdateControllerSoftware && listed === false;
  const [updatable, setUpdatable] = useState<{ answer?: boolean; settled: boolean }>({ settled: false });
  useEffect(() => {
    if (!askServer) return undefined;
    const abort = new AbortController();
    hasUpdatableContainer(abort.signal)
      .then((answer) => {
        if (!abort.signal.aborted) setUpdatable({ answer, settled: true });
      })
      .catch(() => {
        // Unknown keeps the section, as a failed container list does.
        if (!abort.signal.aborted) setUpdatable({ settled: true });
      });
    return () => abort.abort();
  }, [askServer, hasUpdatableContainer]);

  if (!askServer) return { hasContainers: listed, loading: canRead && catalog.loading };
  return { hasContainers: updatable.answer, loading: !updatable.settled };
};

const FirmwareFilesSection = ({
  actionContainer,
  channelsApi,
  refreshWarning,
  onManageChannel,
  showFrigatePackages,
}: {
  actionContainer: HTMLElement | null;
  channelsApi: ReleaseChannelsApi;
  refreshWarning: ReactNode;
  onManageChannel: (channelId: bigint) => void;
  /** Lists controller software under Proto Containers; needs the controller software update permission. */
  showFrigatePackages: boolean;
}) => {
  const { listFirmwareFiles, updateFirmwareMetadata, deleteFirmwareFile, deleteAllFirmwareFiles } = useFirmwareApi();
  const { hasContainers, loading: containersLoading } = usePairedContainers(showFrigatePackages);
  const [files, setFiles] = useState<FirmwareFileData[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [showDeleteAllDialog, setShowDeleteAllDialog] = useState(false);
  const [isDeletingAll, setIsDeletingAll] = useState(false);
  const [fileToDelete, setFileToDelete] = useState<FirmwareFileData | null>(null);
  const [isDeletingSingle, setIsDeletingSingle] = useState(false);
  const [fileToEdit, setFileToEdit] = useState<FirmwareFileData | null>(null);
  const [isEditing, setIsEditing] = useState(false);

  const fileColConfig: typeof colConfig = {
    ...colConfig,
    firmwareVersion: {
      width: "w-36",
      allowWrap: true,
      component: (file) => (
        <div className="flex flex-wrap items-center gap-2">
          <span className="break-all">{file.firmwareVersion || "-"}</span>
          <FirmwareUsageBadge
            checksum={file.checksum}
            filename={file.filename}
            api={channelsApi}
            refreshWarning={refreshWarning}
            onManageChannel={onManageChannel}
          />
        </div>
      ),
    },
  };

  const fetchFiles = useCallback(() => {
    setIsLoading(true);
    listFirmwareFiles()
      .then((fileList) => {
        setFiles(fileList.map(toFileData));
      })
      .catch((error) => {
        pushToast({
          message: error?.message || "Failed to load firmware files",
          status: STATUSES.error,
        });
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, [listFirmwareFiles]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    fetchFiles();
  }, [fetchFiles]);

  const handleDeleteFile = useCallback((file: FirmwareFileData) => {
    setFileToDelete(file);
  }, []);

  const handleEditMetadata = useCallback((file: FirmwareFileData) => {
    setFileToEdit(file);
  }, []);

  const handleEditConfirm = useCallback(
    (metadata: FirmwareMetadataInput) => {
      if (!fileToEdit) return;
      setIsEditing(true);
      updateFirmwareMetadata(fileToEdit.id, metadata)
        .then(() => {
          pushToast({ message: "Firmware metadata updated", status: STATUSES.success });
          setFileToEdit(null);
          fetchFiles();
        })
        .catch((error) => {
          pushToast({
            message: error?.message || "Couldn't update firmware metadata",
            status: STATUSES.error,
          });
        })
        .finally(() => {
          setIsEditing(false);
        });
    },
    [fetchFiles, fileToEdit, updateFirmwareMetadata],
  );

  const handleDeleteFileConfirm = useCallback(() => {
    if (!fileToDelete) return;
    setIsDeletingSingle(true);
    deleteFirmwareFile(fileToDelete.id)
      .then(() => {
        pushToast({
          message: `Deleted ${fileToDelete.filename}`,
          status: STATUSES.success,
        });
        setFileToDelete(null);
        fetchFiles();
      })
      .catch((error) => {
        pushToast({
          message: error?.message || "Failed to delete firmware file",
          status: STATUSES.error,
        });
      })
      .finally(() => {
        setIsDeletingSingle(false);
      });
  }, [fileToDelete, deleteFirmwareFile, fetchFiles]);

  const handleDeleteAllConfirm = useCallback(() => {
    setIsDeletingAll(true);
    deleteAllFirmwareFiles()
      .then((result) => {
        pushToast({
          message: `Deleted ${result.deleted_count} firmware file${result.deleted_count === 1 ? "" : "s"}`,
          status: STATUSES.success,
        });
        setShowDeleteAllDialog(false);
      })
      .catch((error) => {
        pushToast({
          message: error?.message || "Failed to delete all firmware files",
          status: STATUSES.error,
        });
      })
      .finally(() => {
        setIsDeletingAll(false);
        fetchFiles();
      });
  }, [deleteAllFirmwareFiles, fetchFiles]);

  const handleUploadSuccess = useCallback(() => {
    setShowUploadDialog(false);
    fetchFiles();
    pushToast({
      message: "Firmware file uploaded successfully",
      status: STATUSES.success,
    });
  }, [fetchFiles]);

  const availableActions = useMemo(
    () => [
      {
        title: "Edit metadata",
        icon: <Edit />,
        actionHandler: handleEditMetadata,
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: handleDeleteFile,
      },
    ],
    [handleDeleteFile, handleEditMetadata],
  );

  const uploadAction = (
    <Button
      variant={variants.primary}
      size={sizes.compact}
      text="Upload firmware"
      onClick={() => setShowUploadDialog(true)}
      className="shrink-0 phone:w-full"
    />
  );

  const renderFiles = (items: FirmwareFileData[]) => (
    <List<FirmwareFileData, string, FirmwareColumns>
      items={items}
      itemKey="id"
      activeCols={activeCols}
      colTitles={colTitles}
      colConfig={fileColConfig}
      total={items.length}
      itemName={{ singular: "file", plural: "files" }}
      noDataElement={
        <SettingsEmptyState
          title="No firmware files uploaded"
          description="Upload firmware before deploying updates to your fleet."
        />
      }
      actions={availableActions}
    />
  );

  const minerFiles = files.filter((file) => !isContainerFirmware(file));
  const containerFiles = files.filter(isContainerFirmware);
  // Operators with no Proto Container paired see nothing container-specific.
  // An unknown answer keeps the section, so a failed check never hides artifacts.
  const containersApply = !containersLoading && hasContainers !== false;
  const showContainerFiles = containersApply && !isLoading && containerFiles.length > 0;
  const showContainers = containersApply && (showFrigatePackages || showContainerFiles);

  let fileContent: ReactNode = null;
  if (isLoading || containersLoading) {
    fileContent = <div className="text-center text-text-primary-50">Loading firmware files...</div>;
  } else if (!showContainers || files.length === 0) {
    // One ungrouped list of every file, which also carries the empty state.
    fileContent = renderFiles(files);
  } else if (minerFiles.length > 0) {
    fileContent = (
      <FirmwareDeviceSection title="Miners">
        <div>{renderFiles(minerFiles)}</div>
      </FirmwareDeviceSection>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {actionContainer ? createPortal(uploadAction, actionContainer) : uploadAction}
      {files.length > 0 ? (
        <div className="flex justify-end">
          <Button
            variant={variants.danger}
            size={sizes.compact}
            text="Delete all firmware files"
            onClick={() => setShowDeleteAllDialog(true)}
            disabled={isDeletingAll}
            className="phone:w-full"
          />
        </div>
      ) : null}

      {fileContent}
      {showContainers ? (
        <FirmwareDeviceSection title="Proto Containers">
          {showContainerFiles
            ? containerFirmwareSections(containerFiles).map(({ kind, files: kindFiles }) => (
                <FirmwareArtifactSection key={kind.key} title={kind.title}>
                  {renderFiles(kindFiles)}
                </FirmwareArtifactSection>
              ))
            : null}
          {showFrigatePackages ? <FrigatePackages /> : null}
        </FirmwareDeviceSection>
      ) : null}

      <FirmwareUploadDialog
        open={showUploadDialog}
        onSuccess={handleUploadSuccess}
        onDismiss={() => setShowUploadDialog(false)}
      />

      <DeleteFirmwareDialog
        open={fileToDelete !== null}
        filename={fileToDelete?.filename ?? ""}
        onConfirm={handleDeleteFileConfirm}
        onDismiss={() => {
          if (!isDeletingSingle) setFileToDelete(null);
        }}
        isSubmitting={isDeletingSingle}
      />

      <EditFirmwareMetadataDialog
        key={fileToEdit?.id ?? "no-firmware-selected"}
        open={fileToEdit !== null}
        file={fileToEdit}
        isSubmitting={isEditing}
        onConfirm={handleEditConfirm}
        onDismiss={() => {
          if (!isEditing) setFileToEdit(null);
        }}
      />

      <DeleteAllFirmwareDialog
        open={showDeleteAllDialog}
        fileCount={files.length}
        onConfirm={handleDeleteAllConfirm}
        onDismiss={() => setShowDeleteAllDialog(false)}
        isSubmitting={isDeletingAll}
      />
    </div>
  );
};

const TAB_FILES = "files";
const TAB_RELEASE_CHANNELS = "releaseChannels";
export const RELEASE_CHANNELS_TAB_PARAM = "release-channels";
const FRIGATE_UPDATE_PERMISSION = "controller:software_update";
const MINER_FIRMWARE_PERMISSION = "miner:firmware_update";

// FrigatePackagesPage is the page for a user who may update controller
// software but not miner firmware. It never loads miner firmware data, and
// has no tabs because neither miner firmware view is available.
const FrigatePackagesPage = () => (
  <div className="flex flex-col gap-6">
    <SettingsPageHeader title="Firmware" />
    <FirmwareDeviceSection title="Proto Containers">
      <FrigatePackages />
    </FirmwareDeviceSection>
  </div>
);

// The settings route admits either permission; each view checks its own.
const Firmware = () => {
  const canManageMinerFirmware = useHasPermission(MINER_FIRMWARE_PERMISSION);
  const canManageFrigatePackages = useHasPermission(FRIGATE_UPDATE_PERMISSION);
  return !canManageMinerFirmware && canManageFrigatePackages ? <FrigatePackagesPage /> : <MinerFirmwarePage />;
};

// The active tab lives in the `tab` search param so other surfaces can
// deep-link straight to the release channels view. The active-updates monitor
// consumes channel data on both tabs, so one page-owned poll feeds both views.
const MinerFirmwarePage = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const canManageFrigatePackages = useHasPermission(FRIGATE_UPDATE_PERMISSION);
  const activeTab = searchParams.get("tab") === RELEASE_CHANNELS_TAB_PARAM ? TAB_RELEASE_CHANNELS : TAB_FILES;
  const channelsApi = useReleaseChannels();
  const linkedRolloutId = searchParams.get(LINKED_ROLLOUT_PARAM);
  const linkedRollout = useLinkedRollout(linkedRolloutId);
  const [actionContainer, setActionContainer] = useState<HTMLDivElement | null>(null);
  const [isRetrying, setIsRetrying] = useState(false);
  const [isRetryingLinkedChannel, setIsRetryingLinkedChannel] = useState(false);

  const retryChannels = () => {
    if (isRetrying) return;
    setIsRetrying(true);
    // The hook retains the error for the callout if this attempt also fails.
    channelsApi
      .refresh()
      .catch(() => undefined)
      .finally(() => setIsRetrying(false));
  };

  // Let the existing tab handle navigation so an open editor can keep its draft.
  const [manageRequest, setManageRequest] = useState<{ channelId: bigint } | null>(null);
  // Update detail / rollback the history modal asked the monitor to open.
  const [monitorRequest, setMonitorRequest] = useState<MonitorRequest | null>(null);

  const clearLinkedRollout = () => {
    if (linkedRolloutId === null) return;
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete(LINKED_ROLLOUT_PARAM);
        return next;
      },
      { replace: true },
    );
  };
  const linkedChannelAvailable =
    linkedRollout.data && channelsApi.channels.some((channel) => channel.id === linkedRollout.data?.channelId);
  const linkedRequest = useMemo<MonitorRequest | null>(
    () => (linkedChannelAvailable && linkedRollout.data ? { kind: "view", rollout: linkedRollout.data } : null),
    [linkedChannelAvailable, linkedRollout.data],
  );
  const linkedError =
    linkedRollout.error ??
    (linkedRollout.data && channelsApi.hasLoaded && !channelsApi.error && !linkedChannelAvailable
      ? "This firmware update's channel is no longer available."
      : null);
  const isRetryingLink = isRetryingLinkedChannel || linkedRollout.isLoading;
  // Each channel refresh is a full scan, so retry only the read that failed.
  const retryLinkedRollout = () => {
    if (isRetryingLink) return;
    if (linkedRollout.error) {
      linkedRollout.refresh();
      return;
    }
    setIsRetryingLinkedChannel(true);
    channelsApi
      .refresh()
      .catch(() => undefined)
      .finally(() => setIsRetryingLinkedChannel(false));
  };

  const showChannels = () => setSearchParams({ tab: RELEASE_CHANNELS_TAB_PARAM }, { replace: true });
  const manageChannel = (channelId: bigint) => {
    setManageRequest({ channelId });
    showChannels();
  };
  const refreshWarning = channelsApi.error ? (
    <div role="alert" aria-busy={isRetrying}>
      <Callout
        intent={intents.warning}
        prefixIcon={<Alert />}
        title={
          channelsApi.hasLoaded
            ? "Release channels and update status may be out of date"
            : "Couldn't load release channels and update status"
        }
        subtitle={
          channelsApi.hasLoaded ? "Showing the last loaded data. Retry to refresh it." : channelsApi.error.message
        }
        buttonText={isRetrying ? "Retrying..." : "Retry"}
        buttonOnClick={retryChannels}
        testId="release-channels-load-error"
      />
    </div>
  ) : null;

  return (
    <FirmwarePageLayout
      activeTab={activeTab}
      headerAction={<div ref={setActionContainer} className="shrink-0 empty:hidden phone:w-full" />}
      manageRequest={manageRequest}
      refreshWarning={refreshWarning}
      onSelectTab={(key) => {
        if (key === TAB_RELEASE_CHANNELS) {
          showChannels();
        } else {
          setManageRequest(null);
          setSearchParams({}, { replace: true });
        }
      }}
      monitor={
        <>
          {linkedRolloutId !== null && (linkedRollout.isLoading || (!channelsApi.hasLoaded && linkedRollout.data)) ? (
            <p role="status" className="text-text-primary-50">
              Loading firmware update...
            </p>
          ) : null}
          {linkedError ? (
            <div role="alert" aria-busy={isRetryingLink}>
              <Callout
                intent={intents.warning}
                prefixIcon={<Alert />}
                title="Couldn't open firmware update"
                subtitle={linkedError}
                buttonText={isRetryingLink ? "Retrying..." : "Retry"}
                buttonOnClick={retryLinkedRollout}
              />
              <Button text="Dismiss" variant={variants.textOnly} onClick={clearLinkedRollout} />
            </div>
          ) : null}
          <ActiveUpdatesMonitor
            api={channelsApi}
            refreshWarning={refreshWarning}
            request={linkedRequest ?? monitorRequest}
            onRequestHandled={() => {
              setMonitorRequest(null);
              clearLinkedRollout();
            }}
            onManageChannel={manageChannel}
          />
        </>
      }
    >
      {activeTab === TAB_RELEASE_CHANNELS ? (
        <ReleaseChannelsTab
          api={channelsApi}
          actionContainer={actionContainer}
          manageRequest={manageRequest}
          onViewRollout={(rollout) => {
            clearLinkedRollout();
            setMonitorRequest({ kind: "view", rollout });
          }}
          onRollbackRollout={(rollout) => {
            clearLinkedRollout();
            setMonitorRequest({ kind: "rollback", rollout });
          }}
        />
      ) : (
        <FirmwareFilesSection
          actionContainer={actionContainer}
          channelsApi={channelsApi}
          refreshWarning={refreshWarning}
          onManageChannel={manageChannel}
          showFrigatePackages={canManageFrigatePackages}
        />
      )}
    </FirmwarePageLayout>
  );
};

export default Firmware;
