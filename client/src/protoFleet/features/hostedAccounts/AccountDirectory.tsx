import { AccountLayout } from "./AccountLayout";
import { accountPath, roleLabel } from "./capabilities";
import { AccountState } from "./components/AccountState";
import { useHostedAccounts } from "./HostedAccountsContext";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Home } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";

const AccountDirectory = () => {
  const directory = useHostedAccounts();
  if (!directory) return null;
  return (
    <AccountLayout items={[{ path: "/accounts", label: "Home", icon: Home, exact: true }]} roleLabel="Accounts">
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
            activeCols={["name", "parent", "access", "status"]}
            colTitles={{ name: "Account", parent: "Parent", access: "Your access", status: "Status" }}
            colConfig={{
              name: {
                width: "min-w-48",
                component: (account) => (
                  <Button
                    variant="textOnly"
                    text={account.name}
                    to={accountPath(account.accountId)}
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
