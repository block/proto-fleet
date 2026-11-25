import { useCallback, useState } from "react";

import { useSearchParams } from "react-router-dom";
import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { ConfigurationEditor } from "./ConfigurationEditor";
import { ConfigurationUpdates } from "./ConfigurationUpdates";
import { OperationResults } from "./OperationResults";
import type { Configuration } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { PageControls } from "@/protoFleet/features/hostedAccounts/components/PageControls";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import { ConfigurationDetails } from "@/shared/components/PoolConfigurations/ConfigurationDetails";
import { ConfigurationLibraryTable } from "@/shared/components/PoolConfigurations/ConfigurationLibraryTable";
import ProgressCircular from "@/shared/components/ProgressCircular";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

export default function ConfigurationLibrary() {
  const { manage } = usePoolConfigurationPermissions();
  const [search, setSearch] = useSearchParams();
  const operationId = search.get("poolUpdate") ?? "";
  const openOperation = (id: string) => {
    setViewing(undefined);
    setSearch((current) => {
      const next = new URLSearchParams(current);
      if (id) next.set("poolUpdate", id);
      else next.delete("poolUpdate");
      return next;
    });
  };
  const { client, accountId, readOnly } = usePoolConfigurationClient();
  const [tokens, setTokens] = useState([""]);
  const [page, setPage] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listConfigurations(
        { accountId, pageToken: tokens[page], pageSize: 25 },
        { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [client, accountId, page, tokens],
  );
  const query = useAccountQuery(load);
  const [editing, setEditing] = useState<Configuration | "new">();
  const [viewing, setViewing] = useState<Configuration>();
  const [defaulting, setDefaulting] = useState<Configuration>();
  const [deleting, setDeleting] = useState<Configuration>();
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader
          title="Pool configurations"
          description="The default configuration applies when equipment is paired or changes ownership."
        />
        {manage ? (
          <Button
            variant="primary"
            size={sizes.compact}
            disabled={readOnly}
            className="shrink-0 phone:w-full"
            text="Create configuration"
            onClick={() => setEditing("new")}
          />
        ) : null}
      </div>
      {query.loading ? (
        <div className="flex justify-center p-10">
          <ProgressCircular indeterminate />
        </div>
      ) : null}
      {query.error ? (
        <div className="flex flex-col items-center gap-4" role="alert">
          <SettingsEmptyState title="Unable to load configurations" description={query.error} />
          <Button variant="secondary" text="Retry" onClick={query.refresh} />
        </div>
      ) : null}
      {query.data ? (
        <>
          <ConfigurationLibraryTable
            configurations={query.data.configurations}
            onView={setViewing}
            renderActions={
              manage
                ? (configuration) => (
                    <RowActionsMenu
                      ariaLabel={`Actions for ${configuration.name}`}
                      actions={[
                        {
                          label: "Set as default",
                          disabled: readOnly || configuration.isDefault,
                          onClick: () => setDefaulting(configuration),
                        },
                        { label: "Edit configuration", disabled: readOnly, onClick: () => setEditing(configuration) },
                        {
                          label: "Delete configuration",
                          disabled: readOnly,
                          danger: true,
                          onClick: () => setDeleting(configuration),
                        },
                      ]}
                    />
                  )
                : undefined
            }
            emptyState={
              <SettingsEmptyState
                title={page ? "No configurations on this page" : "No pool configurations yet"}
                description={
                  manage
                    ? "Create a configuration with a primary pool and up to two backups."
                    : "Saved configurations will appear here."
                }
              />
            }
          />
          <PageControls
            previous={page > 0}
            next={!!query.data.nextPageToken}
            onPrevious={() => setPage((value) => value - 1)}
            onNext={() => {
              setTokens((values) => [...values.slice(0, page + 1), query.data!.nextPageToken]);
              setPage((value) => value + 1);
            }}
          />
        </>
      ) : null}
      {editing && manage ? (
        <ConfigurationEditor
          key={editing === "new" ? "new" : editing.configurationId}
          original={editing === "new" ? undefined : editing}
          onDismiss={() => setEditing(undefined)}
          onSaved={(_, id) => {
            query.refresh();
            if (id) openOperation(id);
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
          <ConfigurationDetails
            equipment={<p>{viewing.assignedCount.toString()} equipment assigned</p>}
            pools={viewing.pools}
          >
            <ConfigurationUpdates configurationId={viewing.configurationId} onOpen={openOperation} />
          </ConfigurationDetails>
        </Modal>
      ) : null}
      {operationId ? (
        <OperationResults
          key={operationId}
          operationId={operationId}
          onOperationChange={openOperation}
          onDismiss={() => openOperation("")}
        />
      ) : null}
      {defaulting ? (
        <ActionModal
          title="Set default configuration"
          confirm="Set as default"
          onDismiss={() => setDefaulting(undefined)}
          onSubmit={async () => {
            await client.setDefaultConfiguration({
              accountId,
              configurationId: defaulting.configurationId,
              expectedVersion: defaulting.version,
            });
            setDefaulting(undefined);
            query.refresh();
          }}
        >
          <p>
            Use {defaulting.name} for future pairing and ownership changes? Existing equipment will keep its current
            configuration.
          </p>
        </ActionModal>
      ) : null}
      {deleting ? (
        <ActionModal
          title="Delete configuration"
          confirm="Delete configuration"
          valid={deleting.assignedCount === 0n}
          onDismiss={() => setDeleting(undefined)}
          onSubmit={async () => {
            await client.archiveConfiguration({
              accountId,
              configurationId: deleting.configurationId,
              expectedVersion: deleting.version,
            });
            setDeleting(undefined);
            query.refresh();
          }}
        >
          <p>
            {deleting.assignedCount > 0n
              ? "Reassign this configuration’s equipment before deleting it."
              : `Delete ${deleting.name}? It will no longer be available for new assignments.`}
          </p>
          {deleting.isDefault && deleting.assignedCount === 0n ? (
            <p>The oldest remaining configuration will become the default.</p>
          ) : null}
        </ActionModal>
      ) : null}
    </div>
  );
}
