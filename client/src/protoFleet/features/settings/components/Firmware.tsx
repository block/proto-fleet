import { useCallback, useEffect, useMemo, useState } from "react";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import DeleteAllFirmwareDialog from "@/protoFleet/features/settings/components/DeleteAllFirmwareDialog";
import DeleteFirmwareDialog from "@/protoFleet/features/settings/components/DeleteFirmwareDialog";
import FirmwareUploadDialog from "@/protoFleet/features/settings/components/FirmwareUploadDialog";
import { Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

type FirmwareFileData = {
  id: string;
  filename: string;
  size: number;
  uploadedAt: number;
};

type FirmwareColumns = "filename" | "uploadedAt" | "size";

const colTitles: ColTitles<FirmwareColumns> = {
  filename: "File name",
  uploadedAt: "Uploaded",
  size: "Size",
};

const colConfig: ColConfig<FirmwareFileData, string, FirmwareColumns> = {
  filename: {
    component: (file) => <span className="text-emphasis-300">{file.filename}</span>,
    width: "w-60",
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

const activeCols: FirmwareColumns[] = ["filename", "uploadedAt", "size"];

function toFileData(info: FirmwareFileInfo): FirmwareFileData {
  return {
    id: info.id,
    filename: info.filename,
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

  // fetchFiles is a stable callback that internally manages state updates
  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    fetchFiles();
  }, [fetchFiles]);
  /* eslint-enable react-hooks/set-state-in-effect */

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
      <div className="flex items-center justify-between">
        <Header title="Firmware" titleSize="text-heading-300" />
        <div className="flex gap-3">
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Upload firmware"
            onClick={() => setShowUploadDialog(true)}
          />
          <Button
            variant={variants.danger}
            size={sizes.compact}
            text="Delete all"
            onClick={() => setShowDeleteAllDialog(true)}
            disabled={files.length === 0 || isDeletingAll}
          />
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
          noDataElement={<div className="py-10 text-center text-text-primary-50">No firmware files uploaded.</div>}
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
