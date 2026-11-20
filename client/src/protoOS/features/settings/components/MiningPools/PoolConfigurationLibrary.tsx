import { useEffect, useMemo, useRef, useState } from "react";
import PoolConfigurationUpdates, { PoolConfigurationUpdateResults } from "./PoolConfigurationUpdates";
import PoolHashboards, { UnassignedHashboards } from "./PoolHashboards";
import {
  type LocalPoolConfiguration,
  type PoolAssignmentService,
  type PoolAssignmentTarget,
  type PoolConfigurationUpdate,
  PoolDeletionUnconfirmed,
} from "@/protoOS/api/adapters/poolAssignment";
import {
  type ContainerPoolConfiguration,
  getUnassignedPoolAssignmentBlockers,
  hasPendingPoolOperations,
} from "@/protoOS/api/adapters/poolConfiguration";
import PoolAssignmentDialog from "@/protoOS/features/poolAssignment/PoolAssignmentDialog";
import { usePoolAssignmentActions } from "@/protoOS/features/poolAssignment/usePoolAssignmentActions";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { macKey } from "@/protoOS/utils/macAddress";
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
const PAGE_SIZE = 25;

const deleteDisabledReason = (item: LocalPoolConfiguration) => {
  if (item.isDefault) return "This is the controller's fixed default configuration.";
  if (!item.deletable) return "Delete this configuration in Proto Fleet.";
};

export default function PoolConfigurationLibrary({
  service,
  configuration,
  refreshing = false,
  onReload,
}: {
  onReload: () => void;
  service: PoolAssignmentService;
  configuration: ContainerPoolConfiguration;
  refreshing?: boolean;
}) {
  const controllerId = useMinerStore((state) => state.systemInfo.data?.identity.deviceId);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const [data, setData] = useState<{ configurations: LocalPoolConfiguration[]; total: number }>();
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState<LocalPoolConfiguration | "new">();
  const [details, setDetails] = useState<{ configuration: LocalPoolConfiguration; update?: PoolConfigurationUpdate }>();
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
  const poolActions = usePoolAssignmentActions({ onConfigurationSaved: refresh, onAssignmentsChanged: onReload });
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
        const next = await service.listConfigurationPage(page * PAGE_SIZE, PAGE_SIZE, { signal });
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
  const viewing = details?.update ? undefined : details?.configuration;
  const canConfirmDeletion = deleting?.assignedCount === 0 && !deleteError?.unconfirmed;
  const inventoryWarning = configuration.locationsAvailable
    ? configuration.assignmentInventoryError
    : "Could not check for unassigned hashboards.";
  const assignmentBlockers = useMemo(
    () =>
      getUnassignedPoolAssignmentBlockers(
        configuration.inventoryIssues,
        configuration.unassignedBoards.filter(({ isolated }) => isolated).map(({ mac }) => mac),
      ),
    [configuration.inventoryIssues, configuration.unassignedBoards],
  );
  const reportedBoards = useMemo(
    () =>
      new Map(
        [...configuration.groups.flatMap((group) => group.boards), ...configuration.unassignedBoards].map((board) => [
          macKey(board.mac),
          board,
        ]),
      ),
    [configuration],
  );
  const reloadButton = (
    <Button
      variant="secondary"
      text="Reload pools"
      className="self-start"
      onClick={onReload}
      disabled={busy || refreshing}
    />
  );
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Pool configurations" titleSize="text-heading-300" />
        <Button
          variant="primary"
          size="compact"
          disabled={locked || !data || Boolean(error)}
          className="shrink-0 phone:w-full"
          text="Create configuration"
          onClick={() => setEditing("new")}
        />
      </div>
      {configuration.suspended ? (
        <Callout intent="warning" prefixIcon={<Alert />} title="Container mining is paused." />
      ) : null}
      {inventoryWarning ? (
        <div role="alert" className="flex flex-col gap-3">
          <Callout intent="warning" prefixIcon={<Alert />} title={inventoryWarning} />
          {reloadButton}
        </div>
      ) : null}
      {configuration.locationsAvailable ? (
        <UnassignedHashboards
          boards={configuration.unassignedBoards}
          assignmentBlockers={assignmentBlockers}
          poolPickerOpen={poolActions.open}
          onEditPool={
            poolActions.available && !locked && !refreshing && !inventoryWarning
              ? (macs) => poolActions.editPool({ macs, unassignedOnly: true })
              : undefined
          }
        />
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
            onView={(configuration) => setDetails({ configuration })}
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
              <SettingsEmptyState
                title={page ? "No configurations on this page" : "No pool configurations yet"}
                description="Create a configuration with a primary pool and up to two backups."
              />
            }
          />
          <PageControls
            previous={page > 0}
            next={(page + 1) * PAGE_SIZE < data.total}
            onPrevious={() => setPage((value) => value - 1)}
            onNext={() => setPage((value) => value + 1)}
          />
        </>
      ) : null}
      {inventoryWarning ? null : reloadButton}
      {poolActions.recoveryButton}
      {poolActions.dialog}
      {editing && controllerId ? (
        <PoolAssignmentDialog
          key={editing === "new" ? "new" : editing.id}
          service={service}
          controllerId={controllerId}
          target={CONFIGURATION_TARGET}
          configuration={editing}
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
          onDismiss={() => setDetails(undefined)}
        >
          <ConfigurationDetails
            pools={viewing.pools}
            equipment={
              <PoolHashboards
                boards={viewing.assignedMacs.map((mac) => reportedBoards.get(mac) ?? { mac })}
                title={`${viewing.name} hashboards`}
                countLabel="Assigned hashboards"
              />
            }
          >
            <PoolConfigurationUpdates
              key={viewing.id}
              service={service}
              id={viewing.id}
              onView={(update) => setDetails({ configuration: viewing, update })}
            />
          </ConfigurationDetails>
        </Modal>
      ) : null}
      {details?.update ? (
        <PoolConfigurationUpdateResults
          update={details.update}
          configurationName={details.configuration.name}
          onDismiss={() => setDetails(undefined)}
        />
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
