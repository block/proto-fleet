import { useCallback, useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { accountPath, can, capabilities } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { CapabilityFields } from "./CapabilityFields";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";

const AccountChildren = () => {
  const { account, client } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const allowed = account.depth === 0 ? account.capabilityCeiling : account.effectiveCapabilities;
  const required = 9n; // Viewing and membership administration keep recovery possible.
  const canConfigure = (allowed & required) === required;
  const load = useCallback(
    (signal: AbortSignal) => client.listChildren({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const query = useAccountQuery(load);
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
  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title={account.depth === 0 ? "Customers" : "Subclients"} />
      <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
      {query.data ? (
        <List<Account, string, "name" | "status" | "permissions">
          items={query.data.accounts}
          itemKey="accountId"
          itemSelectable={false}
          stickyFirstColumn={false}
          activeCols={["name", "status", "permissions"]}
          colTitles={{ name: "Account", status: "Status", permissions: "Permissions" }}
          colConfig={{
            name: {
              width: "min-w-48",
              component: (child) =>
                can(child, 1n) ? (
                  <Button
                    variant="textOnly"
                    textColor="text-text-primary"
                    className="justify-start text-left font-normal"
                    textOnlyUnderlineOnHover={false}
                    text={child.name}
                    to={accountPath(child.accountId)}
                  />
                ) : (
                  child.name
                ),
            },
            status: { width: "min-w-28", component: (child) => (child.suspended ? "Suspended" : "Active") },
            permissions: {
              width: "min-w-64",
              allowWrap: true,
              component: (child) =>
                capabilities
                  .filter((c) => (child.capabilityCeiling & c.value) !== 0n)
                  .map((c) => c.label)
                  .join(", "),
            },
          }}
          headerControls={
            <Button
              disabled={!canConfigure}
              variant="primary"
              text={account.depth === 0 ? "Create customer" : "Create subclient"}
              onClick={() => open("new")}
            />
          }
          actions={canConfigure ? [{ title: "Manage permissions", actionHandler: open }] : []}
          noDataElement={
            <SettingsEmptyState title="No child accounts yet" description="Create an account, then invite its team." />
          }
          total={query.data.accounts.length}
          itemName={{ singular: "account", plural: "accounts" }}
        />
      ) : null}
      {editing ? (
        <ActionModal
          title={editing === "new" ? "Create account" : `Manage ${editing.name}`}
          confirm={editing === "new" ? "Create account" : "Save permissions"}
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
              <Input id="new-account-name" label="Account name" initValue={name} onChange={setName} maxLength={255} />
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
