import { useLayoutEffect, useState } from "react";

import { AccountLayout } from "./AccountLayout";
import { AccountScopeContext } from "./AccountScope";
import { createAccountConnection } from "./api";
import { accountPath, can, roleLabel } from "./capabilities";
import AccountChildren from "./components/AccountChildren";
import { AccountState } from "./components/AccountState";
import { useHostedAccounts } from "./HostedAccountsContext";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useFleetStore } from "@/protoFleet/store";
import { Groups } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";

const ClientDirectory = ({ root }: { root: Account }) => {
  const directory = useHostedAccounts()!;
  const [connection] = useState(() => createAccountConnection(root.accountId));
  useLayoutEffect(() => {
    connection.activate();
    return () => connection.dispose();
  }, [connection]);
  return (
    <AccountScopeContext.Provider
      value={{
        account: root,
        client: connection.client,
        get signal() {
          return connection.signal;
        },
        refreshAccount: directory.refresh,
        invalidateAccount: directory.refresh,
      }}
    >
      <AccountChildren />
    </AccountScopeContext.Provider>
  );
};

const AccountDirectory = () => {
  const directory = useHostedAccounts();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  if (!directory) return null;
  const root = directory.accounts.find((account) => account.depth === 0 && can(account, 16n));
  if (root)
    return (
      <AccountLayout items={[{ path: "/accounts", label: "Clients", icon: Groups, exact: true }]} roleLabel="Clients">
        <div className="p-10 phone:p-6">
          <ClientDirectory key={`${root.accountId}:${generation}`} root={root} />
        </div>
      </AccountLayout>
    );
  return (
    <AccountLayout items={[{ path: "/accounts", label: "Accounts", icon: Groups, exact: true }]} roleLabel="Accounts">
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="flex items-start justify-between gap-4 phone:flex-col">
          <SettingsPageHeader title="Accounts" />
          <Button variant="secondary" text="Join an account" to="/account-invitation" />
        </div>
        <AccountState loading={directory.loading} error={directory.error} retry={directory.refresh} />
        {!directory.loading && !directory.error ? (
          <List<Account, string, "name" | "parent" | "access" | "status">
            items={directory.accounts}
            itemKey="accountId"
            itemSelectable={false}
            stickyFirstColumn={false}
            activeCols={["name", "parent", "access", "status"]}
            colTitles={{ name: "Account", parent: "Parent", access: "Your access", status: "Status" }}
            colConfig={{
              name: {
                width: "min-w-48",
                component: (account) => (
                  <Button
                    variant="textOnly"
                    textColor="text-text-primary"
                    className="justify-start text-left font-normal"
                    textOnlyUnderlineOnHover={false}
                    text={account.name}
                    to={accountPath(account.accountId, account.depth === 0 ? "children" : "overview")}
                    disabled={account.suspended}
                  />
                ),
              },
              parent: {
                width: "min-w-40",
                component: (account) =>
                  account.depth === 0
                    ? "Host"
                    : (directory.accounts.find((p) => p.accountId === account.parentAccountId)?.name ??
                      "Parent account"),
              },
              access: {
                width: "min-w-44",
                component: (account) => (account.depth === 0 ? "Host administration" : roleLabel(account.role)),
              },
              status: { width: "min-w-28", component: (account) => (account.suspended ? "Suspended" : "Active") },
            }}
            noDataElement={
              <SettingsEmptyState title="No accounts" description="Ask an administrator for an invitation code." />
            }
            total={directory.accounts.length}
            itemName={{ singular: "account", plural: "accounts" }}
          />
        ) : null}
      </div>
    </AccountLayout>
  );
};
export default AccountDirectory;
