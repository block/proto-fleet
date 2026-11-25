import { useCallback, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { accountManagementPath, accountPath, can, capabilities, ticketingDescription } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { useAccountQuery } from "../useAccountQuery";
import AccountAssets from "./AccountAssets";
import { AccountPermissionsEditor } from "./AccountPermissionsEditor";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { CapabilityFields } from "./CapabilityFields";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";

const AccountChildren = () => {
  const { account, client, readOnly = false } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const childTeamPath = (id: string) =>
    account.depth === 0 ? accountManagementPath(id, "team") : accountPath(id, "team");
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const allowed = account.depth === 0 ? account.capabilityCeiling : account.effectiveCapabilities;
  const required = 9n; // Viewing and membership administration keep recovery possible.
  const canConfigure = (allowed & required) === required;
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listChildren({ accountId: account.accountId }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal }),
    [account.accountId, client],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const [params, setParams] = useSearchParams();
  const selectedChild = query.data?.accounts.find((child) => child.accountId === params.get("assets"));
  const assigning = params.get("action") === "assign";
  const [editing, setEditing] = useState<Account | "new" | null>(null);
  const [name, setName] = useState("");
  const [ticketingEnabled, setTicketingEnabled] = useState(true);
  const [ceiling, setCeiling] = useState(1n);
  const open = (child: Account | "new") => {
    setName("");
    setTicketingEnabled(true);
    setCeiling(allowed | required);
    setEditing(child);
  };
  if (params.has("assets")) {
    return (
      <div className="flex min-w-0 flex-col gap-6">
        <div className="flex min-h-10 flex-wrap items-center justify-between gap-3">
          <Breadcrumb
            className="min-w-0 flex-wrap"
            segments={[
              {
                label: account.depth === 0 ? "Clients" : "Subclients",
                to: pathname,
                onSelect: query.refresh,
              },
              {
                label: selectedChild?.name ?? "Mining assets",
                ...(assigning && selectedChild
                  ? { to: `${pathname}?assets=${encodeURIComponent(selectedChild.accountId)}` }
                  : {}),
              },
              ...(assigning ? [{ label: "Allocate assets" }] : []),
            ]}
          />
          <div className="flex flex-wrap gap-3">
            {selectedChild && !assigning && can(selectedChild, 8n) ? (
              <Button
                variant="secondary"
                size={sizes.compact}
                disabled={readOnly}
                text="Invite team member"
                to={`${childTeamPath(selectedChild.accountId)}?invite=1`}
              />
            ) : null}
            {selectedChild && !assigning && can(account, 16n) && account.depth > 0 ? (
              !selectedChild.suspended ? (
                <Button
                  variant="primary"
                  size={sizes.compact}
                  disabled={readOnly}
                  text="Allocate assets"
                  onClick={() => setParams({ assets: selectedChild.accountId, action: "assign" })}
                />
              ) : null
            ) : null}
          </div>
        </div>
        <AccountState
          title={selectedChild ? `Could not refresh ${account.depth === 0 ? "client" : "subclient"}` : undefined}
          loading={query.loading}
          error={query.error}
          retry={query.refresh}
        />
        {selectedChild ? (
          <AccountAssets
            key={`${selectedChild.accountId}:${assigning}`}
            child={selectedChild}
            mode={assigning ? "assign" : "inventory"}
            onAssigned={() => setParams({ assets: selectedChild.accountId })}
          />
        ) : !query.loading && !query.error ? (
          <p>This account is unavailable.</p>
        ) : null}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-6">
      <div className="flex min-h-10 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <SettingsPageHeader
            title={account.depth === 0 ? "Clients" : "Subclients"}
            description={
              account.depth === 0 ? "Manage client accounts, allocated mining assets, and team access." : undefined
            }
          />
        </div>
        {canConfigure ? (
          <Button
            variant="primary"
            size={sizes.compact}
            disabled={readOnly}
            text={account.depth === 0 ? "Create client" : "Create subclient"}
            onClick={() => open("new")}
          />
        ) : null}
      </div>
      <AccountState
        title={query.data ? `Could not refresh ${account.depth === 0 ? "clients" : "subclients"}` : undefined}
        loading={query.loading}
        error={query.error}
        retry={query.refresh}
      />
      {query.data ? (
        <List<Account, string, "name" | "status" | "assets" | "logins" | "permissions">
          items={query.data.accounts}
          itemKey="accountId"
          itemSelectable={false}
          onRowClick={account.depth === 0 ? (child) => navigate(accountManagementPath(child.accountId)) : undefined}
          stickyFirstColumn={false}
          activeCols={account.depth === 0 ? ["name", "status", "assets", "logins"] : ["name", "status", "permissions"]}
          colTitles={{
            name: account.depth === 0 ? "Client" : "Subclient",
            status: "Status",
            assets: "Allocated assets",
            logins: "Logins",
            permissions: "Permissions",
          }}
          colConfig={{
            name: {
              width: account.depth === 0 ? "min-w-40" : "min-w-48",
              component: (child) => (
                <Button
                  variant="textOnly"
                  textColor="text-text-primary"
                  className="justify-start text-left font-normal"
                  textOnlyUnderlineOnHover={false}
                  text={child.name}
                  to={
                    account.depth === 0
                      ? accountManagementPath(child.accountId)
                      : can(child, 1n)
                        ? accountPath(child.accountId)
                        : `?assets=${encodeURIComponent(child.accountId)}`
                  }
                />
              ),
            },
            status: {
              width: account.depth === 0 ? "min-w-24" : "min-w-28",
              component: (child) => (child.suspended ? "Suspended" : "Active"),
            },
            assets: {
              width: "min-w-48",
              component: (child) => {
                const counts = child.managementSummary;
                if (!counts) return "—";
                const types: string[] = [];
                for (const [count, label] of [
                  [counts.minerCount, "miner"],
                  [counts.hashboardCount, "hashboard"],
                  [counts.moduleCount, "module"],
                ] as const) {
                  if (count > 0n) types.push(`${count.toLocaleString()} ${label}${count === 1n ? "" : "s"}`);
                }
                return (
                  <div className="flex flex-col gap-1">
                    <span>{types.length ? types.join(" · ") : "No mining assets"}</span>
                    {counts.containerCount > 0n ? (
                      <span className="text-200 text-text-primary-50">
                        Equipment in {counts.containerCount.toLocaleString()}{" "}
                        {counts.containerCount === 1n ? "container" : "containers"}
                      </span>
                    ) : null}
                  </div>
                );
              },
            },
            logins: {
              width: "min-w-20",
              component: (child) => child.managementSummary?.loginCount.toLocaleString() ?? "—",
            },
            permissions: {
              width: account.depth === 0 ? "min-w-48" : "min-w-64",
              allowWrap: true,
              component: (child) =>
                capabilities
                  .filter((c) => (child.capabilityCeiling & c.value) !== 0n)
                  .map((c) => c.label)
                  .join(", "),
            },
          }}
          actions={[
            ...(account.depth === 1 && can(account, 16n)
              ? [
                  {
                    title: "Allocate assets",
                    actionHandler: (child: Account) => {
                      if (!child.suspended) setParams({ assets: child.accountId, action: "assign" });
                    },
                    disabled: (child: Account) => readOnly || child.suspended,
                  },
                ]
              : []),
            {
              title: "View mining assets",
              actionHandler: (child: Account) =>
                account.depth === 0
                  ? navigate(accountManagementPath(child.accountId))
                  : setParams({ assets: child.accountId }),
            },
            {
              title: account.depth === 0 ? "View as client" : "Open workspace",
              actionHandler: (child: Account) => {
                navigate(accountPath(child.accountId));
              },
              hidden: (child: Account) => !can(child, 1n),
            },
            {
              title: "Invite team member",
              disabled: readOnly,
              actionHandler: (child: Account) => navigate(`${childTeamPath(child.accountId)}?invite=1`),
              hidden: (child: Account) => !can(child, 8n),
            },
            ...(account.depth === 0
              ? [
                  {
                    title: "Client settings",
                    actionHandler: (child: Account) => navigate(accountManagementPath(child.accountId, "settings")),
                    hidden: (child: Account) => !can(child, 1n),
                  },
                ]
              : []),
            ...(canConfigure
              ? [
                  {
                    title: "Manage permissions",
                    disabled: readOnly,
                    actionHandler: (child: Account) =>
                      account.depth === 0
                        ? navigate(accountManagementPath(child.accountId, "permissions"))
                        : open(child),
                  },
                ]
              : []),
          ]}
          noDataElement={
            <SettingsEmptyState
              title={account.depth === 0 ? "No clients yet" : "No subclients yet"}
              description={
                account.depth === 0
                  ? "Create a client, allocate mining assets, then invite its team."
                  : "Create a subclient, then invite its team."
              }
            />
          }
          total={query.data.accounts.length}
          itemName={
            account.depth === 0
              ? { singular: "client", plural: "clients" }
              : { singular: "subclient", plural: "subclients" }
          }
        />
      ) : null}
      {editing === "new" ? (
        <ActionModal
          title={account.depth === 0 ? "Create client" : "Create subclient"}
          confirm={account.depth === 0 ? "Create client" : "Create subclient"}
          valid={!!name.trim() && (ceiling & required) === required ? (ceiling & ~allowed) === 0n : false}
          onDismiss={() => setEditing(null)}
          onSubmit={async () => {
            const result = await client.createAccount({
              accountId: account.accountId,
              name: name.trim(),
              capabilities: ceiling,
              ticketingEnabled,
            });
            if (result.account)
              navigate(
                account.depth === 0
                  ? accountManagementPath(result.account.accountId)
                  : `${pathname}?assets=${encodeURIComponent(result.account.accountId)}`,
              );
            setEditing(null);
            query.invalidate();
            directory?.refresh();
          }}
        >
          <Input
            id="new-account-name"
            label={account.depth === 0 ? "Client name" : "Subclient name"}
            initValue={name}
            onChange={setName}
            maxLength={255}
          />
          <CapabilityFields value={ceiling} allowed={allowed} required={required} onChange={setCeiling} />
          <label className="flex items-start gap-3 text-300 text-text-primary">
            <Checkbox
              ariaLabel="Ticketing"
              className="shrink-0"
              checked={ticketingEnabled}
              onChange={(event) => setTicketingEnabled(event.target.checked)}
            />
            <span>
              Ticketing
              <span className="mt-1 block text-200 text-text-primary-50">{ticketingDescription}</span>
            </span>
          </label>
        </ActionModal>
      ) : editing ? (
        <AccountPermissionsEditor
          child={editing}
          onDismiss={() => setEditing(null)}
          onSaved={() => {
            query.refresh();
            directory?.refresh();
          }}
        />
      ) : null}
    </div>
  );
};
export default AccountChildren;
