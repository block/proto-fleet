import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { type FirmwareFileInfo, type FirmwareMetadataInput, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import DeleteAllFirmwareDialog from "@/protoFleet/features/settings/components/DeleteAllFirmwareDialog";
import DeleteFirmwareDialog from "@/protoFleet/features/settings/components/DeleteFirmwareDialog";
import EditFirmwareMetadataDialog from "@/protoFleet/features/settings/components/EditFirmwareMetadataDialog";
import FirmwareUploadDialog from "@/protoFleet/features/settings/components/FirmwareUploadDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { ChevronDown, Edit, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
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
  firmwareVersion: {
    component: (file) => <span>{file.firmwareVersion || "-"}</span>,
    width: "w-36",
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
const FIRMWARE_PAGE_DESCRIPTION = "Upload and manage firmware files available to your fleet.";

function toFileData(info: FirmwareFileInfo): FirmwareFileData {
  return {
    id: info.id,
    filename: info.filename,
    targetManufacturer: info.target_manufacturer,
    targetModel: info.target_model,
    firmwareVersion: info.firmware_version ?? "",
    size: info.size,
    uploadedAt: isoToEpochSeconds(info.uploaded_at),
  };
}

const Firmware = () => {
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

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader title="Firmware" description={FIRMWARE_PAGE_DESCRIPTION} />
        <div className="flex shrink-0 gap-3 phone:w-full phone:flex-col">
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Upload firmware"
            onClick={() => setShowUploadDialog(true)}
            className="phone:w-full"
          />
          {files.length > 0 ? (
            <Button
              variant={variants.danger}
              size={sizes.compact}
              text="Delete all"
              onClick={() => setShowDeleteAllDialog(true)}
              disabled={isDeletingAll}
              className="phone:w-full"
            />
          ) : null}
        </div>
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading firmware files...</div>
      ) : (
        <List<FirmwareFileData, string, FirmwareColumns>
          items={files}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
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

export default Firmware;
