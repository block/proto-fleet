import { useCallback, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";

import { useRequiredAccountScope } from "../AccountScope";
import { accountPath, can, capabilities } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { useAccountQuery } from "../useAccountQuery";
import AccountAssets from "./AccountAssets";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { CapabilityFields } from "./CapabilityFields";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";

const AccountChildren = () => {
  const { account, client } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const allowed = account.depth === 0 ? account.capabilityCeiling : account.effectiveCapabilities;
  const required = 9n; // Viewing and membership administration keep recovery possible.
  const canConfigure = (allowed & required) === required;
  const load = useCallback(
    (signal: AbortSignal) => client.listChildren({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const query = useAccountQuery(load, true, true);
  const [params, setParams] = useSearchParams();
  const selectedChild = query.data?.accounts.find((child) => child.accountId === params.get("assets"));
  const assigning = params.get("action") === "assign";
  const [editing, setEditing] = useState<Account | "new" | null>(null);
  const [name, setName] = useState("");
  const [ceiling, setCeiling] = useState(1n);
  const [suspended, setSuspended] = useState(false);
  const open = (child: Account | "new") => {
    setName("");
    setCeiling(child === "new" ? allowed | required : (child.capabilityCeiling & allowed) | required);
    setSuspended(child !== "new" && child.suspended);
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
              ...(assigning ? [{ label: "Assign assets" }] : []),
            ]}
          />
          {selectedChild && !assigning && can(account, 16n) ? (
            account.depth === 0 ? (
              <Button variant="primary" text="Assign from Fleet" to="/fleet/miners" />
            ) : !selectedChild.suspended ? (
              <Button
                variant="primary"
                text="Assign assets"
                onClick={() => setParams({ assets: selectedChild.accountId, action: "assign" })}
              />
            ) : null
          ) : null}
        </div>
        <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
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
      <div className="flex min-h-10 flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <SettingsPageHeader title={account.depth === 0 ? "Clients" : "Subclients"} />
        </div>
        {canConfigure ? (
          <Button
            variant="primary"
            text={account.depth === 0 ? "Create client" : "Create subclient"}
            onClick={() => open("new")}
          />
        ) : null}
      </div>
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <List<Account, string, "name" | "status" | "assets" | "logins" | "permissions">
          items={query.data.accounts}
          itemKey="accountId"
          itemSelectable={false}
          stickyFirstColumn={false}
          activeCols={
            account.depth === 0
              ? ["name", "status", "assets", "logins", "permissions"]
              : ["name", "status", "permissions"]
          }
          colTitles={{
            name: account.depth === 0 ? "Client" : "Subclient",
            status: "Status",
            assets: "Assigned assets",
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
                  to={`?assets=${encodeURIComponent(child.accountId)}`}
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
                const types = [
                  ...(counts.minerCount > 0n
                    ? [`${counts.minerCount.toLocaleString()} ${counts.minerCount === 1n ? "miner" : "miners"}`]
                    : []),
                  ...(counts.moduleCount > 0n
                    ? [`${counts.moduleCount.toLocaleString()} ${counts.moduleCount === 1n ? "module" : "modules"}`]
                    : []),
                ];
                return (
                  <div className="flex flex-col gap-1">
                    <span>{types.length ? types.join(" · ") : "No assets assigned"}</span>
                    {counts.moduleCount > 0n ? (
                      <span className="text-200 text-text-primary-50">
                        Modules in {counts.containerCount.toLocaleString()}{" "}
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
                    title: "Assign assets",
                    actionHandler: (child: Account) => {
                      if (!child.suspended) setParams({ assets: child.accountId, action: "assign" });
                    },
                    disabled: (child: Account) => child.suspended,
                  },
                ]
              : []),
            { title: "View mining assets", actionHandler: (child: Account) => setParams({ assets: child.accountId }) },
            {
              title: "Open workspace",
              actionHandler: (child: Account) => {
                navigate(accountPath(child.accountId));
              },
              hidden: (child: Account) => !can(child, 1n),
            },
            ...(canConfigure ? [{ title: "Manage permissions", actionHandler: open }] : []),
          ]}
          noDataElement={
            <SettingsEmptyState
              title={account.depth === 0 ? "No clients yet" : "No subclients yet"}
              description={
                account.depth === 0
                  ? "Create a client, then invite its team and assign mining assets from Fleet."
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
      {editing ? (
        <ActionModal
          title={
            editing === "new" ? (account.depth === 0 ? "Create client" : "Create subclient") : `Manage ${editing.name}`
          }
          confirm={
            editing === "new" ? (account.depth === 0 ? "Create client" : "Create subclient") : "Save permissions"
          }
          valid={
            (editing !== "new" || !!name.trim()) && (ceiling & required) === required
              ? (ceiling & ~allowed) === 0n
              : false
          }
          onDismiss={() => setEditing(null)}
          onSubmit={async () => {
            if (editing === "new")
              await client.createAccount({ accountId: account.accountId, name: name.trim(), capabilities: ceiling });
            else
              await client.configureAccount({
                accountId: account.accountId,
                childAccountId: editing.accountId,
                capabilities: ceiling,
                suspended,
              });
            setEditing(null);
            query.refresh();
            directory?.refresh();
          }}
        >
          {editing === "new" ? (
            <>
              <Input
                id="new-account-name"
                label={account.depth === 0 ? "Client name" : "Subclient name"}
                initValue={name}
                onChange={setName}
                maxLength={255}
              />
              <p className="text-200 text-text-primary-50">
                You’ll be the first administrator. You can then invite the team.
              </p>
            </>
          ) : null}
          <CapabilityFields value={ceiling} allowed={allowed} required={required} onChange={setCeiling} />
          <p className="text-200 text-text-primary-50">Viewing data and managing the team are required.</p>
          {editing !== "new" ? (
            <label className="flex items-start gap-3">
              <Checkbox checked={suspended} onChange={(e) => setSuspended(e.target.checked)} />
              <span>
                Suspend account
                <p className="text-200 text-text-primary-50">
                  Blocks this account and its subclients. Miner pools stay unchanged.
                </p>
              </span>
            </label>
          ) : null}
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountChildren;
