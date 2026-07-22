import { useCallback, useEffect, useMemo, useState } from "react";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import DeleteAllFirmwareDialog from "@/protoFleet/features/settings/components/DeleteAllFirmwareDialog";
import DeleteFirmwareDialog from "@/protoFleet/features/settings/components/DeleteFirmwareDialog";
import FirmwareUploadDialog from "@/protoFleet/features/settings/components/FirmwareUploadDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

type FirmwareFileData = {
  id: string;
  filename: string;
  target: string;
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

const colConfig: ColConfig<FirmwareFileData, string, FirmwareColumns> = {
  filename: {
    component: (file) => <span className="text-emphasis-300">{file.filename}</span>,
    width: "w-60",
  },
  target: {
    component: (file) => <span>{file.target}</span>,
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
    target: `${info.target_manufacturer} ${info.target_model}`.trim() || "Unknown",
    firmwareVersion: info.firmware_version ?? "",
    size: info.size,
    uploadedAt: isoToEpochSeconds(info.uploaded_at),
  };
}

const Firmware = () => {
  const { listFirmwareFiles, deleteFirmwareFile, deleteAllFirmwareFiles } = useFirmwareApi();
  const [files, setFiles] = useState<FirmwareFileData[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showUploadDialog, setShowUploadDialog] = useState(false);
  const [showDeleteAllDialog, setShowDeleteAllDialog] = useState(false);
  const [isDeletingAll, setIsDeletingAll] = useState(false);
  const [fileToDelete, setFileToDelete] = useState<FirmwareFileData | null>(null);
  const [isDeletingSingle, setIsDeletingSingle] = useState(false);

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
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: handleDeleteFile,
      },
    ],
    [handleDeleteFile],
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
