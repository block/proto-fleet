import { useEffect, useMemo, useRef, useState } from "react";
import {
  type LocalPoolConfiguration,
  type LocalPoolConfigurationPage,
  MINING_GROUP_LIMIT_MESSAGE,
  type PoolAssignmentService,
  type PoolAssignmentTarget,
  PoolDeletionUnconfirmed,
} from "@/protoOS/api/adapters/poolAssignment";
import { type ContainerPoolConfiguration, hasPendingPoolOperations } from "@/protoOS/api/adapters/poolConfiguration";
import PoolAssignmentDialog from "@/protoOS/features/poolAssignment/PoolAssignmentDialog";
import { usePoolAssignmentActions } from "@/protoOS/features/poolAssignment/usePoolAssignmentActions";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { Alert } from "@/shared/assets/icons";
import { ActionModal } from "@/shared/components/ActionModal";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import { PageControls } from "@/shared/components/PageControls";
import { ConfigurationDetails } from "@/shared/components/PoolConfigurations/ConfigurationDetails";
import { ConfigurationLibraryTable } from "@/shared/components/PoolConfigurations/ConfigurationLibraryTable";
import ProgressCircular from "@/shared/components/ProgressCircular";
import RowActionsMenu from "@/shared/components/RowActionsMenu";
import SettingsEmptyState from "@/shared/components/SettingsEmptyState";
import { usePoll } from "@/shared/hooks/usePoll";

const CONFIGURATION_TARGET: PoolAssignmentTarget = { macs: [] };

const deleteDisabledReason = (item: LocalPoolConfiguration) => {
  if (item.isDefault) return "This is the controller's fixed default configuration.";
  if (!item.deletable) return "Delete this configuration in Proto Fleet.";
};

export default function PoolConfigurationLibrary({
  service,
  configuration,
  onReload,
}: {
  onReload: () => void;
  service: PoolAssignmentService;
  configuration: ContainerPoolConfiguration;
}) {
  const controllerId = useMinerStore((state) => state.systemInfo.data?.identity.deviceId);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const [data, setData] = useState<LocalPoolConfigurationPage>();
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState<LocalPoolConfiguration | "new">();
  const [viewing, setViewing] = useState<LocalPoolConfiguration>();
  const [deleting, setDeleting] = useState<LocalPoolConfiguration>();
  const [deleteError, setDeleteError] = useState<{ message: string; unconfirmed: boolean }>();
  const [busy, setBusy] = useState(false);
  const deletion = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => deletion.current?.abort(), [service, generation]);
  const [page, setPage] = useState(0);
  const locked = !controllerId || configuration.suspended || hasPendingPoolOperations(configuration.pendingOperations);
  const refresh = () => {
    setData(undefined);
    setError(undefined);
    setRevision((value) => value + 1);
  };
  const recovery = usePoolAssignmentActions({ onPoolsChanged: refresh, showRecovery: true });
  const deleteSelectedConfiguration = async () => {
    if (busy || !deleting) return;
    const request = new AbortController();
    deletion.current = request;
    setBusy(true);
    setDeleteError(undefined);
    try {
      await service.deleteConfiguration(deleting.id, deleting.revision, { signal: request.signal });
      if (request.signal.aborted) return;
      setDeleting(undefined);
      setPage(0);
      refresh();
    } catch (cause) {
      if (request.signal.aborted) return;
      setDeleteError({
        message: cause instanceof Error ? cause.message : "Could not delete the configuration.",
        unconfirmed: cause instanceof PoolDeletionUnconfirmed,
      });
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  };
  const query = useMemo(() => ({ service, revision, generation, page }), [service, revision, generation, page]);
  usePoll({
    enabled: true,
    poll: false,
    params: query,
    fetchData: async (signal) => {
      try {
        const next = await service.listConfigurationPage(page * 25, 25, { signal });
        if (!signal.aborted) {
          setData(next);
          setError(undefined);
        }
      } catch (cause) {
        if (!signal.aborted) setError(cause instanceof Error ? cause.message : "Could not load configurations.");
      }
    },
  });
  const configurations = (data?.configurations ?? []).map((item) => ({ ...item, configurationId: item.id }));
  const noGroupSpace = data && data.capacity.groupsUsed >= data.capacity.maxGroups;
  const canConfirmDeletion = deleting?.assignedCount === 0 && !deleteError?.unconfirmed;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Pool configurations" titleSize="text-heading-300" />
        <Button
          variant="primary"
          size="compact"
          disabled={locked || !data || noGroupSpace || Boolean(error)}
          className="shrink-0 phone:w-full"
          text="Create configuration"
          onClick={() => setEditing("new")}
        />
      </div>
      {noGroupSpace ? <p role="status">{MINING_GROUP_LIMIT_MESSAGE}</p> : null}
      {configuration.suspended ? (
        <Callout intent="warning" prefixIcon={<Alert />} title="Container mining is paused." />
      ) : null}
      {error ? (
        <div role="alert" className="flex flex-col items-center gap-4">
          <SettingsEmptyState title="Unable to load configurations" description={error} />
          <Button variant="secondary" text="Retry" onClick={refresh} />
        </div>
      ) : null}
      {!data && !error ? (
        <div className="flex justify-center p-10">
          <ProgressCircular indeterminate />
        </div>
      ) : null}
      {data ? (
        <>
          <ConfigurationLibraryTable
            configurations={configurations}
            onView={setViewing}
            renderActions={(item) => (
              <RowActionsMenu
                ariaLabel={`Actions for ${item.name}`}
                actions={[
                  {
                    label: "Edit configuration",
                    disabled: locked || Boolean(error),
                    onClick: () => setEditing(item),
                  },
                  {
                    label: "Delete configuration",
                    disabled: locked || Boolean(error) || !item.deletable,
                    disabledReason: deleteDisabledReason(item),
                    danger: true,
                    onClick: () => {
                      setDeleteError(undefined);
                      setDeleting(item);
                    },
                  },
                ]}
              />
            )}
            emptyState={
              <SettingsEmptyState title={page ? "No configurations on this page" : "No pool configurations yet"} />
            }
          />
          <PageControls
            previous={page > 0}
            next={(page + 1) * 25 < data.total}
            onPrevious={() => setPage((value) => value - 1)}
            onNext={() => setPage((value) => value + 1)}
          />
        </>
      ) : null}
      <Button variant="secondary" text="Reload pools" className="self-start" onClick={onReload} disabled={busy} />
      {recovery.recoveryButton}
      {recovery.dialog}
      {editing && controllerId && data ? (
        <PoolAssignmentDialog
          key={editing === "new" ? "new" : editing.id}
          service={service}
          controllerId={controllerId}
          target={CONFIGURATION_TARGET}
          configuration={editing}
          configurationCapacity={data.capacity}
          onDismiss={() => setEditing(undefined)}
          onConfigurationSaved={() => {
            setEditing(undefined);
            refresh();
          }}
        />
      ) : null}
      {viewing ? (
        <Modal
          open
          title={viewing.name}
          dialogLabel="Pool configuration"
          manageFocus
          onDismiss={() => setViewing(undefined)}
        >
          <ConfigurationDetails assignedCount={viewing.assignedCount} pools={viewing.pools}></ConfigurationDetails>
        </Modal>
      ) : null}
      {deleting ? (
        <ActionModal
          title="Delete configuration"
          confirm="Delete configuration"
          valid={canConfirmDeletion}
          cancel={deleteError?.unconfirmed ? "Close" : "Cancel"}
          busy={busy}
          error={deleteError?.message}
          onDismiss={() => {
            if (busy) return;
            setDeleting(undefined);
            if (deleteError?.unconfirmed) refresh();
          }}
          onSubmit={() => void deleteSelectedConfiguration()}
        >
          <p>
            {deleting.assignedCount
              ? "Reassign this configuration’s equipment before deleting it."
              : `Delete ${deleting.name}? It will no longer be available for new assignments.`}
          </p>
        </ActionModal>
      ) : null}
    </div>
  );
}
