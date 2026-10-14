import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";

import { AccountLayout } from "./AccountLayout";
import { AccountScopeContext } from "./AccountScope";
import { createAccountConnection } from "./api";
import { accountPath, can, roleLabel } from "./capabilities";
import AccountAccess from "./components/AccountAccess";
import AccountChildren from "./components/AccountChildren";
import AccountPools from "./components/AccountPools";
import AccountSettings from "./components/AccountSettings";
import { AccountState } from "./components/AccountState";
import AccountTeam from "./components/AccountTeam";
import { useHostedAccounts } from "./HostedAccountsContext";
import { useAccountQuery } from "./useAccountQuery";
import { type NavItem } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";
import { Groups, LightningAlt, Lock, Settings } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";

const AccountPage = ({ accountId }: { accountId: string }) => {
  const { section = "settings" } = useParams();
  const directory = useHostedAccounts();
  const [connection] = useState(() => createAccountConnection(accountId));
  useEffect(() => {
    connection.activate();
    return () => connection.dispose();
  }, [connection]);

  const load = useCallback(
    (signal: AbortSignal) => {
      return connection.client.getAccount({ accountId }, { signal });
    },
    [accountId, connection],
  );
  // The root summary carries host delegation authority; GetAccount resolves
  // private customer authority and intentionally rejects depth zero.
  const rootAccount = directory?.accounts.find((item) => item.accountId === accountId && item.depth === 0);
  const query = useAccountQuery(load, !directory?.loading && !rootAccount, true);
  const { invalidate: invalidateQuery, refresh: refreshQuery } = query;
  const directoryRefresh = directory?.refresh;
  const invalidate = useCallback(() => {
    invalidateQuery();
    directoryRefresh?.();
  }, [invalidateQuery, directoryRefresh]);
  const refresh = useCallback(() => {
    refreshQuery();
    directoryRefresh?.();
  }, [refreshQuery, directoryRefresh]);
  useEffect(() => {
    window.addEventListener("focus", query.refresh);
    return () => window.removeEventListener("focus", query.refresh);
  }, [query.refresh]);
  const account = rootAccount ?? query.data?.account;
  const error = directory?.error ?? query.error;
  const loading = directory?.loading || (!account && !error && query.loading);
  const navItems: NavItem[] = [
    { path: accountPath(accountId), label: "Settings", icon: Settings },
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: accountPath(accountId, "pools"), label: "Pools", icon: LightningAlt }]
      : []),
    ...(account && account.depth > 0 && can(account, 8n)
      ? [{ path: accountPath(accountId, "team"), label: "Team", icon: Groups }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: accountPath(accountId, "access"), label: "Access", icon: Lock }]
      : []),
    ...(can(account, 16n) && account!.depth < 2
      ? [
          {
            path: accountPath(accountId, "children"),
            label: account!.depth === 0 ? "Customers" : "Subclients",
            icon: Groups,
          },
        ]
      : []),
  ];
  const accessible = navItems.some((item) => item.path === accountPath(accountId, section));
  return (
    <AccountLayout
      items={navItems}
      roleLabel={account?.depth === 0 ? "Host administration" : account ? roleLabel(account.role) : "Account workspace"}
    >
      <div className="p-10 phone:p-6">
        {loading ? (
          <div className="flex justify-center p-10">
            <ProgressCircular indeterminate />
          </div>
        ) : !account && error ? (
          <>
            <AccountState error={error} retry={refresh} />
            <Link className="text-text-emphasis" to="/accounts">
              Back to accounts
            </Link>
          </>
        ) : account ? (
          <AccountScopeContext.Provider
            value={{
              account,
              client: connection.client,
              signal: connection.signal,
              refreshAccount: refresh,
              invalidateAccount: invalidate,
            }}
          >
            {error ? <AccountState error={error} retry={refresh} /> : null}
            {accessible ? (
              section === "team" ? (
                <AccountTeam />
              ) : section === "pools" ? (
                <AccountPools />
              ) : section === "children" ? (
                <AccountChildren />
              ) : section === "access" ? (
                <AccountAccess />
              ) : (
                <AccountSettings
                  onRenamed={() => {
                    refreshQuery();
                    directory?.refresh();
                  }}
                />
              )
            ) : (
              <AccountSettings
                onRenamed={() => {
                  refreshQuery();
                  directory?.refresh();
                }}
              />
            )}
          </AccountScopeContext.Provider>
        ) : (
          <AccountState error="This account is unavailable." retry={query.refresh} />
        )}
      </div>
    </AccountLayout>
  );
};
const AccountWorkspace = () => {
  const { accountId = "" } = useParams();
  const generation = useFleetStore((s) => s.auth.sessionGeneration);
  return <AccountPage key={`${accountId}:${generation}`} accountId={accountId} />;
};
export default AccountWorkspace;
