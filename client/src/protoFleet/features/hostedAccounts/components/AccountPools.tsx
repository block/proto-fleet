import { useCallback, useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { PageControls } from "./PageControls";
import { PoolRevisionEditor } from "./PoolRevisionEditor";
import type { PoolRevision } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";

const AccountPools = () => {
  const { account, client } = useRequiredAccountScope();
  const [tokens, setTokens] = useState([""]),
    [page, setPage] = useState(0);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [list, incoming] = await Promise.all([
        client.listPoolConfigurations(
          { accountId: account.accountId, pageSize: 25, pageToken: tokens[page] },
          { signal },
        ),
        client.getIncomingPoolProfile({ accountId: account.accountId }, { signal }),
      ]);
      const profile = incoming.profile;
      const pinned =
        profile?.poolId && profile.revisionId
          ? (
              await client.getPoolRevision(
                { accountId: account.accountId, poolId: profile.poolId, revisionId: profile.revisionId },
                { signal },
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
      <SettingsPageHeader title="Pool configurations" />
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <>
          <div className="rounded-xl border border-border-10 p-6">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="text-heading-200 text-text-primary">Incoming miners</h2>
                <p className="mt-1 text-300 text-text-primary-70">Pools for newly assigned miners.</p>
              </div>
              {change ? (
                <Button
                  variant="secondary"
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
            <Row className="flex items-center justify-between">
              <span>Selected revision</span>
              {query.data.pinned ? (
                <Button
                  variant="textOnly"
                  text={`${query.data.pinned.name} · revision ${query.data.pinned.number}`}
                  onClick={() => setViewing(query.data!.pinned!)}
                />
              ) : (
                <span className="text-text-primary-50">Not selected</span>
              )}
            </Row>
            <p className="text-200 text-text-primary-50">Existing miner pools stay unchanged.</p>
          </div>
          <List<PoolRevision, string, "name" | "number" | "primary" | "backups">
            items={query.data.configurations}
            itemKey="poolId"
            itemSelectable={false}
            activeCols={["name", "number", "primary", "backups"]}
            colTitles={{
              name: "Configuration",
              number: "Revision",
              primary: "Primary pool",
              backups: "Backups",
            }}
            colConfig={{
              name: {
                width: "min-w-40",
                component: (pool) => <Button variant="textOnly" text={pool.name} onClick={() => setViewing(pool)} />,
              },
              number: { width: "min-w-24", component: (pool) => String(pool.number) },
              primary: {
                width: "min-w-56",
                allowWrap: true,
                component: (pool) => <span className="break-all">{pool.slots[0]?.url}</span>,
              },
              backups: { width: "min-w-24", component: (pool) => String(Math.max(0, pool.slots.length - 1)) },
            }}
            headerControls={
              manage ? <Button variant="primary" text="Create configuration" onClick={() => setEditing("new")} /> : null
            }
            actions={
              manage
                ? [
                    { title: "Edit configuration", actionHandler: setEditing },
                    { title: "Archive", variant: "destructive", actionHandler: setArchiving },
                  ]
                : []
            }
            noDataElement={
              <SettingsEmptyState
                title="No configurations on this page"
                description={
                  manage
                    ? "Create a primary pool with optional backups."
                    : "An account pool operator can create configurations."
                }
              />
            }
          />
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
              This configuration is selected for incoming miners. Change or clear the selection before archiving.
            </p>
          ) : null}
        </ActionModal>
      ) : null}
      {selecting && change ? (
        <ActionModal
          title="Incoming pool configuration"
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
          <p className="text-200 text-text-primary-50">This changes future assignments, not existing miner pools.</p>
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountPools;
