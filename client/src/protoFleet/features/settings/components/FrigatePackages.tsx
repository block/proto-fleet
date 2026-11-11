import { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import type { FrigatePackage } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFrigatePackages } from "@/protoFleet/api/useFrigatePackages";
import FrigatePackageUploadDialog from "@/protoFleet/features/settings/components/FrigatePackageUploadDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import { Alert, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type PackageColumns = "version" | "architecture" | "sha256" | "releaseKeyId" | "uploadedAt";

type PackageRow = {
  id: string;
  filename: string;
  version: string;
  architecture: string;
  sha256: string;
  releaseKeyId: string;
  sizeBytes: number;
  uploadedAt: number;
  storedOnThisHost: boolean;
};

const colTitles: ColTitles<PackageColumns> = {
  version: "Version",
  architecture: "Architecture",
  sha256: "Digest",
  releaseKeyId: "Signer key",
  uploadedAt: "Uploaded",
};

const activeCols: PackageColumns[] = ["version", "architecture", "sha256", "releaseKeyId", "uploadedAt"];

// Digests and key IDs are long hex strings: show the start and keep the whole value in the title.
const Hash = ({ value }: { value: string }) => (
  <span className="font-mono text-200" title={value}>
    {value.length > 22 ? `${value.slice(0, 22)}…` : value}
  </span>
);

const colConfig: ColConfig<PackageRow, string, PackageColumns> = {
  version: {
    component: (row) => (
      <div className="flex flex-col">
        <span className="text-emphasis-300">{row.version}</span>
        <span className="text-200 text-text-primary-70">
          {row.filename}, {formatFileSize(row.sizeBytes)}
        </span>
        {row.storedOnThisHost ? null : (
          <span className="text-200 text-intent-warning-text">
            Stored on another Fleet host. Upload it again to use it from here.
          </span>
        )}
      </div>
    ),
    width: "w-64",
    allowWrap: true,
  },
  architecture: { component: (row) => <span>{row.architecture}</span>, width: "w-32" },
  sha256: { component: (row) => <Hash value={row.sha256} />, width: "w-56" },
  releaseKeyId: { component: (row) => <Hash value={row.releaseKeyId} />, width: "w-56" },
  uploadedAt: { component: (row) => <span>{formatTimestamp(row.uploadedAt)}</span>, width: "w-48" },
};

function toRow(item: FrigatePackage): PackageRow {
  return {
    id: item.id,
    filename: item.filename,
    version: item.version,
    architecture: item.architecture,
    sha256: item.sha256,
    releaseKeyId: item.releaseKeyId,
    sizeBytes: Number(item.sizeBytes),
    uploadedAt: item.uploadedAt ? Number(item.uploadedAt.seconds) : 0,
    storedOnThisHost: item.storedOnThisHost,
  };
}

const FrigatePackages = ({ actionContainer }: { actionContainer: HTMLElement | null }) => {
  const { listPackages, deletePackage } = useFrigatePackages();
  const [rows, setRows] = useState<PackageRow[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string>();
  const [showUpload, setShowUpload] = useState(false);
  const [toDelete, setToDelete] = useState<PackageRow | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string>();

  const fetchPackages = useCallback(
    (signal?: AbortSignal) => {
      setIsLoading(true);
      listPackages(signal)
        .then((items) => {
          if (signal?.aborted) return;
          setRows(items.map(toRow));
          setLoadError(undefined);
        })
        .catch((error) => {
          if (signal?.aborted) return;
          setLoadError(getErrorMessage(error, "Couldn't load Frigate packages."));
        })
        .finally(() => {
          if (!signal?.aborted) setIsLoading(false);
        });
    },
    [listPackages],
  );

  useEffect(() => {
    const abort = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    fetchPackages(abort.signal);
    return () => abort.abort();
  }, [fetchPackages]);

  const confirmDelete = useCallback(async () => {
    if (!toDelete || isDeleting) return;
    setIsDeleting(true);
    setDeleteError(undefined);
    try {
      await deletePackage(toDelete.id);
      pushToast({ message: `Deleted ${toDelete.filename}`, status: STATUSES.success });
      setToDelete(null);
      fetchPackages();
    } catch (error) {
      // The server refuses while an update that uses the package is in progress.
      setDeleteError(getErrorMessage(error, "Couldn't delete the package."));
    } finally {
      setIsDeleting(false);
    }
  }, [deletePackage, fetchPackages, isDeleting, toDelete]);

  const actions = useMemo(
    () => [
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: (row: PackageRow) => {
          setDeleteError(undefined);
          setToDelete(row);
        },
      },
    ],
    [],
  );

  const uploadAction = (
    <Button
      variant={variants.primary}
      size={sizes.compact}
      text="Upload Frigate package"
      onClick={() => setShowUpload(true)}
      className="shrink-0 phone:w-full"
    />
  );

  return (
    <div className="flex flex-col gap-6">
      {actionContainer ? createPortal(uploadAction, actionContainer) : uploadAction}
      {loadError ? (
        <p role="alert" className="text-300 text-intent-critical-text">
          {loadError}
        </p>
      ) : null}
      {isLoading && rows.length === 0 ? (
        <div className="text-center text-text-primary-50">Loading Frigate packages...</div>
      ) : (
        <List<PackageRow, string, PackageColumns>
          items={rows}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          total={rows.length}
          itemName={{ singular: "package", plural: "packages" }}
          noDataElement={
            <SettingsEmptyState
              title="No Frigate packages uploaded"
              description="Upload a signed Frigate release to update container controllers."
            />
          }
          actions={actions}
        />
      )}

      <FrigatePackageUploadDialog
        open={showUpload}
        onDismiss={() => setShowUpload(false)}
        onSuccess={(result) => {
          setShowUpload(false);
          fetchPackages();
          pushToast({
            message: result.reused ? "That package was already uploaded" : "Frigate package uploaded",
            status: STATUSES.success,
          });
        }}
      />

      <Dialog
        open={toDelete !== null}
        title="Delete Frigate package?"
        testId="delete-frigate-package-dialog"
        onDismiss={() => {
          if (!isDeleting) setToDelete(null);
        }}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          { text: "Cancel", onClick: () => setToDelete(null), variant: variants.secondary, disabled: isDeleting },
          { text: "Delete", onClick: () => void confirmDelete(), variant: variants.danger, loading: isDeleting },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          This deletes "{toDelete?.filename}" for every site. Past updates keep their history. It can't be deleted while
          an update that uses it is in progress.
        </div>
        {deleteError ? (
          <p role="alert" className="mt-3 text-intent-critical-text">
            {deleteError}
          </p>
        ) : null}
      </Dialog>
    </div>
  );
};

export default FrigatePackages;
