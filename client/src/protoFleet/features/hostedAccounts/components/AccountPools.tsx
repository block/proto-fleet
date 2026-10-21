import { useCallback, useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { PageControls } from "./PageControls";
import { PoolRevisionEditor } from "./PoolRevisionEditor";
import type { PoolRevision } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";

const AccountPools = () => {
  const { account, client, readOnly = false } = useRequiredAccountScope();
  const [tokens, setTokens] = useState([""]),
    [page, setPage] = useState(0);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [list, incoming] = await Promise.all([
        client.listPoolConfigurations(
          { accountId: account.accountId, pageSize: 25, pageToken: tokens[page] },
          { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
        ),
        client.getIncomingPoolProfile({ accountId: account.accountId }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal }),
      ]);
      const profile = incoming.profile;
      const pinned =
        profile?.poolId && profile.revisionId
          ? (
              await client.getPoolRevision(
                { accountId: account.accountId, poolId: profile.poolId, revisionId: profile.revisionId },
                { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
              )
            ).revision
          : undefined;
      return { ...list, profile, pinned };
    },
    [account.accountId, client, page, tokens],
  );
  const query = useAccountQuery(load);
  const [editing, setEditing] = useState<PoolRevision | "new" | null>(null);
  const [viewing, setViewing] = useState<PoolRevision | null>(null);
  const [archiving, setArchiving] = useState<PoolRevision | null>(null);
  const [selecting, setSelecting] = useState(false),
    [revisionId, setRevisionId] = useState(""),
    [password, setPassword] = useState(""),
    [clear, setClear] = useState(false);
  const manage = can(account, 2n),
    change = can(account, 4n);
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="min-w-0 flex-1">
          <SettingsPageHeader title="Pools" description="Add and manage the pools for your account." />
        </div>
        {query.data && manage ? (
          <Button
            variant="primary"
            size={sizes.compact}
            disabled={readOnly}
            className="shrink-0 phone:w-full"
            text="Add pool"
            onClick={() => setEditing("new")}
          />
        ) : null}
      </div>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          <List<PoolRevision, string, "name" | "url" | "username">
            items={query.data.configurations}
            itemKey="poolId"
            itemSelectable={false}
            stickyFirstColumn={false}
            applyColumnWidthsToCells
            tableClassName="mb-0"
            activeCols={["name", "url", "username"]}
            colTitles={{ name: "Name", url: "URL", username: "Username" }}
            colConfig={{
              name: {
                width: "w-1/3 min-w-40",
                component: (pool) => (
                  <div className="flex flex-col items-start">
                    <Button
                      variant="textOnly"
                      textColor="text-text-primary"
                      className="justify-start text-left font-normal"
                      textOnlyUnderlineOnHover={false}
                      text={pool.name}
                      onClick={() => setViewing(pool)}
                    />
                    {pool.slots.length > 1 ? (
                      <span className="text-200 text-text-primary-50">
                        {pool.slots.length - 1} backup{pool.slots.length > 2 ? "s" : ""}
                      </span>
                    ) : null}
                  </div>
                ),
              },
              url: {
                width: "w-1/3 min-w-56",
                allowWrap: true,
                component: (pool) => <span className="break-all">{pool.slots[0]?.url || "—"}</span>,
              },
              username: {
                width: "w-1/3 min-w-40",
                allowWrap: true,
                component: (pool) => <span className="break-all">{pool.slots[0]?.username || "—"}</span>,
              },
            }}
            actions={
              manage
                ? [
                    { title: "Edit configuration", disabled: readOnly, actionHandler: setEditing },
                    { title: "Archive", disabled: readOnly, variant: "destructive", actionHandler: setArchiving },
                  ]
                : []
            }
            noDataElement={
              <SettingsEmptyState
                title={page > 0 ? "No pools on this page" : "No pools yet"}
                description={manage ? "Add a pool with optional backups." : "An account pool operator can add pools."}
              />
            }
          />
          {query.data.configurations.length || query.data.pinned ? (
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-5 pt-4">
              <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
                <span className="text-300 text-text-primary-70">Stored pool profile</span>
                {query.data.pinned ? (
                  <Button
                    variant="textOnly"
                    text={`${query.data.pinned.name} · revision ${query.data.pinned.number}`}
                    onClick={() => setViewing(query.data!.pinned!)}
                  />
                ) : (
                  <span className="text-300 text-text-primary-50">Not selected</span>
                )}
              </div>
              {change ? (
                <Button
                  variant="secondary"
                  size={sizes.compact}
                  disabled={readOnly}
                  text="Change selection"
                  onClick={() => {
                    setRevisionId("");
                    setClear(false);
                    setPassword("");
                    setSelecting(true);
                  }}
                />
              ) : null}
            </div>
          ) : null}
          <PageControls
            previous={page > 0}
            next={!!query.data.nextPageToken}
            onPrevious={() => setPage((p) => p - 1)}
            onNext={() => {
              setTokens((t) => [...t.slice(0, page + 1), query.data!.nextPageToken]);
              setPage((p) => p + 1);
            }}
          />
        </>
      ) : null}
      {editing && manage ? (
        <PoolRevisionEditor
          key={editing === "new" ? "new" : editing.revisionId}
          original={editing === "new" ? undefined : editing}
          onDismiss={() => setEditing(null)}
          onSaved={query.refresh}
        />
      ) : null}
      {viewing ? (
        <Modal
          open
          title={`${viewing.name} · revision ${viewing.number}`}
          dialogLabel="Pool configuration revision"
          onDismiss={() => setViewing(null)}
          manageFocus
        >
          <div className="flex flex-col gap-6">
            {viewing.slots.map((slot, i) => (
              <div key={slot.priority}>
                <h3 className="text-heading-100">{i === 0 ? "Primary pool" : `Backup ${i}`}</h3>
                <Row className="flex flex-col gap-1">
                  <span className="text-200 text-text-primary-50">Destination</span>
                  <span className="break-all">{slot.url}</span>
                </Row>
                <Row>Payout username: {slot.username}</Row>
                <Row>Worker suffix: {slot.workerTemplate || "None"}</Row>
                <Row>{slot.hasPassword ? "Password saved" : "No password"}</Row>
              </div>
            ))}
          </div>
        </Modal>
      ) : null}
      {archiving && manage ? (
        <ActionModal
          title="Archive configuration"
          confirm="Archive"
          onDismiss={() => setArchiving(null)}
          onSubmit={async () => {
            await client.archivePoolConfiguration({ accountId: account.accountId, poolId: archiving.poolId });
            setArchiving(null);
            query.refresh();
          }}
        >
          <p>Archive {archiving.name}? Existing revisions remain readable. Miner pools are unchanged.</p>
          {query.data?.profile?.poolId === archiving.poolId ? (
            <p className="text-text-warning">
              This configuration is selected as the stored pool profile. Change or clear the selection before archiving.
            </p>
          ) : null}
        </ActionModal>
      ) : null}
      {selecting && change ? (
        <ActionModal
          title="Stored pool profile"
          confirm="Confirm selection"
          valid={password ? Boolean(clear || revisionId) : false}
          onDismiss={() => {
            setPassword("");
            setSelecting(false);
          }}
          onSubmit={async () => {
            await client.setIncomingPoolProfile({
              accountId: account.accountId,
              revisionId: clear ? undefined : revisionId,
              password,
            });
            setPassword("");
            setSelecting(false);
            query.refresh();
          }}
        >
          <Select
            id="hosted-incoming-revision"
            label="Revision"
            value={revisionId}
            onChange={setRevisionId}
            disabled={clear}
            options={(query.data?.configurations ?? []).map((p) => ({
              value: p.revisionId,
              label: `${p.name} · revision ${p.number}`,
            }))}
          />
          <label className="flex items-center gap-3">
            <Checkbox checked={clear} onChange={(event) => setClear(event.target.checked)} />
            Clear selection
          </label>
          <Input
            id="hosted-profile-password"
            label="Your Fleet password"
            type="password"
            initValue={password}
            onChange={setPassword}
            autoComplete="current-password"
            maxLength={72}
          />
          <p className="text-200 text-text-primary-50">
            This changes the stored profile. It does not apply pool settings to existing or newly assigned miners or
            modules.
          </p>
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountPools;
