import { type ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { createPortal } from "react-dom";
import { type FirmwareFileInfo, type FirmwareMetadataInput, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { type ReleaseChannelsApi, useReleaseChannels } from "@/protoFleet/api/useReleaseChannels";
import DeleteAllFirmwareDialog from "@/protoFleet/features/settings/components/DeleteAllFirmwareDialog";
import DeleteFirmwareDialog from "@/protoFleet/features/settings/components/DeleteFirmwareDialog";
import EditFirmwareMetadataDialog from "@/protoFleet/features/settings/components/EditFirmwareMetadataDialog";
import FirmwarePageLayout from "@/protoFleet/features/settings/components/FirmwarePageLayout";
import FirmwareUploadDialog from "@/protoFleet/features/settings/components/FirmwareUploadDialog";
import FirmwareUsageBadge from "@/protoFleet/features/settings/components/FirmwareUsageBadge";
import ActiveUpdatesMonitor, {
  type MonitorRequest,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ActiveUpdatesMonitor";
import ReleaseChannelsTab from "@/protoFleet/features/settings/components/ReleaseChannels/ReleaseChannelsTab";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { Alert, ChevronDown, Edit, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

type FirmwareFileData = {
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
    checksum: info.sha256 ?? "",
    size: info.size,
    uploadedAt: isoToEpochSeconds(info.uploaded_at),
  };
}

const FirmwareFilesSection = ({
  actionContainer,
  channelsApi,
  refreshWarning,
  onManageChannel,
}: {
  actionContainer: HTMLElement | null;
  channelsApi: ReleaseChannelsApi;
  refreshWarning: ReactNode;
  onManageChannel: (channelId: bigint) => void;
}) => {
  const { listFirmwareFiles, updateFirmwareMetadata, deleteFirmwareFile, deleteAllFirmwareFiles } = useFirmwareApi();
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

  return (
    <div className="flex flex-col gap-6">
      {actionContainer ? createPortal(uploadAction, actionContainer) : uploadAction}
      {files.length > 0 ? (
        <div className="flex justify-end">
          <Button
            variant={variants.danger}
            size={sizes.compact}
            text="Delete all"
            onClick={() => setShowDeleteAllDialog(true)}
            disabled={isDeletingAll}
            className="phone:w-full"
          />
        </div>
      ) : null}

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading firmware files...</div>
      ) : (
        <List<FirmwareFileData, string, FirmwareColumns>
          items={files}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={fileColConfig}
          total={files.length}
          itemName={{ singular: "file", plural: "files" }}
          noDataElement={
            <SettingsEmptyState
              title="No firmware files uploaded"
              description="Upload firmware before deploying updates to your fleet."
            />
          }
          actions={availableActions}
        />
      )}

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

// The active tab lives in the `tab` search param so other surfaces can
// deep-link straight to the release channels view. The active-updates monitor
// consumes channel data on both tabs, so one page-owned poll feeds both views.
const Firmware = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get("tab") === RELEASE_CHANNELS_TAB_PARAM ? TAB_RELEASE_CHANNELS : TAB_FILES;
  const channelsApi = useReleaseChannels();
  const [actionContainer, setActionContainer] = useState<HTMLDivElement | null>(null);
  const [isRetrying, setIsRetrying] = useState(false);

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
        <ActiveUpdatesMonitor
          api={channelsApi}
          refreshWarning={refreshWarning}
          request={monitorRequest}
          onRequestHandled={() => setMonitorRequest(null)}
          onManageChannel={manageChannel}
        />
      }
    >
      {activeTab === TAB_RELEASE_CHANNELS ? (
        <ReleaseChannelsTab
          api={channelsApi}
          actionContainer={actionContainer}
          manageRequest={manageRequest}
          onViewRollout={(rollout) => setMonitorRequest({ kind: "view", rollout })}
          onRollbackRollout={(rollout) => setMonitorRequest({ kind: "rollback", rollout })}
        />
      ) : (
        <FirmwareFilesSection
          actionContainer={actionContainer}
          channelsApi={channelsApi}
          refreshWarning={refreshWarning}
          onManageChannel={manageChannel}
        />
      )}
    </FirmwarePageLayout>
  );
};

export default Firmware;
