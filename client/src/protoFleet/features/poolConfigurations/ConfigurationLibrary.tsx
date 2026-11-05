import { useCallback, useState } from "react";

import { useSearchParams } from "react-router-dom";
import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { ConfigurationEditor } from "./ConfigurationEditor";
import { ConfigurationUpdates } from "./ConfigurationUpdates";
import { poolSlotLabel } from "./draft";
import { OperationResults } from "./OperationResults";
import type { Configuration } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { PageControls } from "@/protoFleet/features/hostedAccounts/components/PageControls";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes } from "@/shared/components/Button";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
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
  const [deleting, setDeleting] = useState<Configuration>();
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader
          title="Pool configurations"
          description="Save a primary pool and optional backups to apply to your equipment."
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
          <List<Configuration, string, "name" | "destination" | "assigned" | "actions">
            items={query.data.configurations}
            itemKey="configurationId"
            itemSelectable={false}
            stickyFirstColumn={false}
            applyColumnWidthsToCells
            tableClassName="mb-0"
            activeCols={manage ? ["name", "destination", "assigned", "actions"] : ["name", "destination", "assigned"]}
            colTitles={{ name: "Name", destination: "Primary pool", assigned: "Assigned equipment", actions: "" }}
            colConfig={{
              name: {
                width: "w-1/3 min-w-40",
                component: (configuration) => (
                  <div className="flex flex-col items-start">
                    <Button
                      variant="textOnly"
                      textColor="text-text-primary"
                      className="justify-start text-left font-normal"
                      textOnlyUnderlineOnHover={false}
                      text={configuration.name}
                      onClick={() => setViewing(configuration)}
                    />
                    {configuration.pools.length > 1 ? (
                      <span className="text-200 text-text-primary-50">
                        {configuration.pools.length - 1} backup{configuration.pools.length > 2 ? "s" : ""}
                      </span>
                    ) : null}
                  </div>
                ),
              },
              destination: {
                width: "w-1/3 min-w-56",
                allowWrap: true,
                component: (configuration) => <span className="break-all">{configuration.pools[0]?.url || "—"}</span>,
              },
              assigned: {
                width: "w-1/3 min-w-40",
                component: (configuration) => <span>{configuration.assignedCount.toString()}</span>,
              },
              actions: {
                width: "w-12",
                component: (configuration) => (
                  <RowActionsMenu
                    ariaLabel={`Actions for ${configuration.name}`}
                    actions={[
                      { label: "Edit configuration", disabled: readOnly, onClick: () => setEditing(configuration) },
                      {
                        label: "Delete configuration",
                        disabled: readOnly,
                        danger: true,
                        onClick: () => setDeleting(configuration),
                      },
                    ]}
                  />
                ),
              },
            }}
            noDataElement={
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
          <div className="flex flex-col gap-6" data-dd-privacy="mask">
            <p>{viewing.assignedCount.toString()} equipment assigned</p>
            <ConfigurationUpdates configurationId={viewing.configurationId} onOpen={openOperation} />
            {viewing.pools.map((pool, index) => (
              <div key={pool.slotId} className="flex flex-col gap-2 break-all">
                <h3 className="text-heading-100">{poolSlotLabel(index)}</h3>
                <p>{pool.url}</p>
                <p>Username: {pool.username}</p>
                <p>{pool.hasPassword ? "Password saved" : "No password"}</p>
              </div>
            ))}
          </div>
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
        </ActionModal>
      ) : null}
    </div>
  );
}
