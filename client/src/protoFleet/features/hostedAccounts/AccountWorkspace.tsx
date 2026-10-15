import { useCallback, useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";

import { AccountLayout } from "./AccountLayout";
import { AccountScopeContext } from "./AccountScope";
import { createAccountConnection } from "./api";
import { accountPath, can, roleLabel } from "./capabilities";
import AccountAccess from "./components/AccountAccess";
import AccountAssets from "./components/AccountAssets";
import AccountChildren from "./components/AccountChildren";
import AccountOverview from "./components/AccountOverview";
import AccountPools from "./components/AccountPools";
import AccountSettings from "./components/AccountSettings";
import { AccountState } from "./components/AccountState";
import AccountTeam from "./components/AccountTeam";
import { useHostedAccounts } from "./HostedAccountsContext";
import { useAccountQuery } from "./useAccountQuery";
import { type NavItem } from "@/protoFleet/config/navItems";
import { useFleetStore, usePermissions } from "@/protoFleet/store";
import { Activity, Fleet, Groups, LightningAlt, Lock, Settings } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

const AccountPage = ({ accountId }: { accountId: string }) => {
  const { section } = useParams();
  const directory = useHostedAccounts();
  const permissions = usePermissions();
  const host = permissions.length > 0 || !!directory?.hostAccess;
  const navigate = useNavigate();
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
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: accountPath(accountId), label: "Overview", icon: Activity }]
      : []),
    ...(account && ((account.depth > 0 && can(account, 1n)) || (account.depth === 0 && can(account, 16n)))
      ? [{ path: accountPath(accountId, "assets"), label: "Mining assets", icon: Fleet }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: accountPath(accountId, "pools"), label: "Pools", icon: LightningAlt }]
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
    ...(account && account.depth > 0 && can(account, 8n)
      ? [{ path: accountPath(accountId, "team"), label: "Team", icon: Groups }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: accountPath(accountId, "access"), label: "Access", icon: Lock }]
      : []),
    ...(account && (account.depth === 0 || can(account, 1n))
      ? [{ path: accountPath(accountId, "settings"), label: "Settings", icon: Settings }]
      : []),
  ];
  const defaultPath = accountPath(accountId, account?.depth === 0 ? "children" : "overview");
  const accessible = !!section && navItems.some((item) => item.path === accountPath(accountId, section));
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
            {host ? (
              <div className="mb-6 overflow-x-auto">
                <TabStrip
                  ariaLabel="Account sections"
                  activeId={accessible ? accountPath(accountId, section) : defaultPath}
                  onSelect={navigate}
                >
                  {navItems.map((item) => (
                    <TabStripItem key={item.path} id={item.path} label={item.label} />
                  ))}
                </TabStrip>
              </div>
            ) : null}
            {error ? <AccountState error={error} retry={refresh} /> : null}
            {accessible ? (
              section === "overview" ? (
                <AccountOverview key={`${account.effectiveCapabilities}:${account.suspended}`} />
              ) : section === "assets" ? (
                <AccountAssets key={`${account.effectiveCapabilities}:${account.suspended}`} />
              ) : section === "team" ? (
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
              <Navigate to={navItems.some((item) => item.path === defaultPath) ? defaultPath : "/accounts"} replace />
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
