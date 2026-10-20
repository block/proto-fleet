import { useCallback, useEffect, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";

import { AccountLayout } from "./AccountLayout";
import { AccountScopeProvider } from "./AccountScopeProvider";
import { createAccountConnection } from "./api";
import { accountManagementPath, accountPath, can, roleLabel } from "./capabilities";
import AccountAccess from "./components/AccountAccess";
import AccountActivity from "./components/AccountActivity";
import AccountAssetDetail from "./components/AccountAssetDetail";
import AccountAssets from "./components/AccountAssets";
import AccountChildren from "./components/AccountChildren";
import AccountDetails from "./components/AccountDetails";
import AccountOverview from "./components/AccountOverview";
import AccountPools from "./components/AccountPools";
import { AccountState } from "./components/AccountState";
import AccountTeam from "./components/AccountTeam";
import { useHostedAccounts } from "./HostedAccountsContext";
import { useAccountQuery } from "./useAccountQuery";
import { type NavItem, type SecondaryNavItem } from "@/protoFleet/config/navItems";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import Preferences from "@/protoFleet/features/settings/components/Preferences";
import { useFleetStore, usePermissions } from "@/protoFleet/store";
import { Activity, Fleet, Groups, Home, LightningAlt, Lock, Settings, Team } from "@/shared/assets/icons";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

const AccountPage = ({
  accountId,
  management = false,
  onPreviewClose,
  onAccountChanged,
  embedded = false,
}: {
  accountId: string;
  management?: boolean;
  onPreviewClose?: () => void;
  onAccountChanged?: () => void;
  embedded?: boolean;
}) => {
  const { section: sectionParam, kind, assetId } = useParams();
  const section = kind && assetId ? "assets" : sectionParam;
  const directory = useHostedAccounts();
  const permissions = usePermissions();
  const host = permissions.length > 0 || !!directory?.hostAccess;
  const navigate = useNavigate();
  const rootAccount = directory?.accounts.find((item) => item.accountId === accountId && item.depth === 0);
  const hostAdministration = directory?.accounts.some((item) => item.depth === 0 && can(item, 16n));
  const preview = !!hostAdministration && !rootAccount && !management;
  const path = (id: string, page = "overview") =>
    management ? accountManagementPath(id, page) : accountPath(id, page);
  const [connection] = useState(() => createAccountConnection(accountId, { readOnly: preview }));
  useEffect(() => {
    connection.activate();
    return () => connection.dispose();
  }, [connection]);

  const load = useCallback(
    async (signal: AbortSignal) => {
      const [result, policy] = await Promise.all([
        connection.client.getAccount({ accountId }, { signal }),
        connection.client.getSitePolicy({ accountId }, { signal }),
      ]);
      return { ...result, policy };
    },
    [accountId, connection],
  );
  // The root summary carries host delegation authority; GetAccount resolves
  // private customer authority and intentionally rejects depth zero.
  const query = useAccountQuery(load, !directory?.loading && !rootAccount, true, POLL_INTERVAL_MS);
  const { invalidate: invalidateQuery, refresh: refreshQuery } = query;
  const directoryRefresh = directory?.refresh;
  const invalidate = useCallback(() => {
    invalidateQuery();
    directoryRefresh?.();
    onAccountChanged?.();
  }, [invalidateQuery, directoryRefresh, onAccountChanged]);
  const refresh = useCallback(() => {
    refreshQuery();
    directoryRefresh?.();
    onAccountChanged?.();
  }, [refreshQuery, directoryRefresh, onAccountChanged]);
  useEffect(() => {
    window.addEventListener("focus", query.refresh);
    return () => window.removeEventListener("focus", query.refresh);
  }, [query.refresh]);
  const account = rootAccount ?? query.data?.account;
  const authority = account
    ? `${account.effectiveCapabilities}:${account.capabilityCeiling}:${account.suspended}:${account.revision}:${query.data?.policy.effectiveRestricted}:${query.data?.policy.effectiveSiteIds.map(String).sort().join(",")}`
    : "";
  const parent = directory?.accounts.find(
    (item) => item.accountId === account?.parentAccountId && item.depth > 0 && can(item, 1n),
  );
  const error = directory?.error ?? query.error;
  const loading = directory?.loading || (!account && !error && query.loading);
  const navItems: NavItem[] = [
    ...(account && account.depth > 0 && can(account, 1n) ? [{ path: path(accountId), label: "Home", icon: Home }] : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: path(accountId, "assets"), label: "Fleet", icon: Fleet }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: path(accountId, "activity"), label: "Activity", icon: Activity }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: path(accountId, "pools"), label: "Pools", icon: LightningAlt }]
      : []),
    ...(can(account, 16n) && account!.depth < 2
      ? [
          {
            path: path(accountId, "children"),
            label: account!.depth === 0 ? "Clients" : "Subclients",
            icon: Groups,
          },
        ]
      : []),
    ...(account && account.depth > 0 && can(account, 8n)
      ? [{ path: path(accountId, "team"), label: "Team", icon: Team }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: path(accountId, "access"), label: "Access", icon: Lock }]
      : []),
    ...(account && account.depth > 0 && can(account, 1n)
      ? [{ path: path(accountId, "preferences"), label: "Preferences" }]
      : []),
    ...(account?.depth === 0 || management
      ? [{ path: path(accountId, "settings"), label: management ? "Client settings" : "Settings", icon: Settings }]
      : []),
  ].filter((item) => !management || ["team", "settings"].some((page) => item.path === path(accountId, page)));
  const settingsPath = path(accountId, "settings");
  const settingsPages: SecondaryNavItem[] = ["pools", "team", "access", "children", "preferences"].flatMap((page) => {
    const item = navItems.find((item) => item.path === path(accountId, page));
    return item && account?.depth !== 0
      ? [
          {
            path: item.path,
            label: item.label,
            parent: settingsPath,
            section: page === "pools" ? "Fleet" : page === "preferences" ? "Account" : "Admin",
          },
        ]
      : [];
  });
  const workspaceItems: NavItem[] =
    account?.depth !== 0 && !management
      ? [
          ...navItems.filter((item) =>
            [path(accountId), path(accountId, "assets"), path(accountId, "activity")].includes(item.path),
          ),
          ...(settingsPages.length
            ? [{ path: settingsPath, label: "Settings", icon: Settings, children: settingsPages }]
            : []),
        ]
      : navItems;
  const defaultPath = path(accountId, account?.depth === 0 ? "children" : management ? "team" : "overview");
  const accessible = !!section && navItems.some((item) => item.path === path(accountId, section));
  const renderSection = (authorityKey: string) => {
    switch (section) {
      case "activity":
        return <AccountActivity key={authorityKey} />;
      case "overview":
        return <AccountOverview key={authorityKey} />;
      case "assets":
        if (kind && assetId) return <AccountAssetDetail key={`${kind}:${assetId}`} />;
        return <AccountAssets key={authorityKey} />;
      case "team":
        return <AccountTeam />;
      case "pools":
        return <AccountPools />;
      case "children":
        return <AccountChildren />;
      case "access":
        return <AccountAccess />;
      case "settings":
        if (management) return <AccountAccess title="Client settings" />;
        break;
      case "preferences":
        return <Preferences />;
    }
    return (
      <div className="flex flex-col gap-6">
        <h1 className="text-heading-300">Settings</h1>
        <AccountDetails />
      </div>
    );
  };
  const content = (
    <div className={embedded ? undefined : "p-10 phone:p-6"}>
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
        <AccountScopeProvider
          key={authority}
          account={account}
          readOnly={preview}
          refreshAccount={refresh}
          invalidateAccount={invalidate}
        >
          {account.depth > 0 && !preview && !embedded ? (
            <div className="mb-6 flex min-h-10 flex-wrap items-center justify-between gap-3">
              <Breadcrumb
                className="min-w-0 flex-wrap"
                segments={[
                  ...(host ? [{ label: "Clients", to: "/accounts" }] : []),
                  ...(parent ? [{ label: parent.name, to: accountPath(parent.accountId) }] : []),
                  { label: account.name },
                ]}
              />
              {management ? (
                <Button variant="secondary" size={sizes.compact} text="View as client" to={accountPath(accountId)} />
              ) : null}
            </div>
          ) : null}
          {!embedded && host && (rootAccount || management) ? (
            <div className="mb-6 overflow-x-auto">
              <TabStrip
                ariaLabel="Account sections"
                activeId={accessible ? path(accountId, section) : defaultPath}
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
            renderSection(`${account.effectiveCapabilities}:${account.suspended}`)
          ) : (
            <Navigate
              to={
                section === "settings" && settingsPages.length
                  ? settingsPages[0].path
                  : navItems.some((item) => item.path === defaultPath)
                    ? defaultPath
                    : "/accounts"
              }
              replace
            />
          )}
        </AccountScopeProvider>
      ) : (
        <AccountState error="This account is unavailable." retry={query.refresh} />
      )}
    </div>
  );
  return embedded ? (
    content
  ) : (
    <AccountLayout
      items={workspaceItems}
      workspace={rootAccount ? false : !management}
      previewName={preview ? (account?.name ?? "client") : undefined}
      onPreviewClose={onPreviewClose ?? (() => navigate("/accounts", { replace: true }))}
      roleLabel={
        preview
          ? "Account administrator preview"
          : account?.depth === 0
            ? "Host administration"
            : account
              ? roleLabel(account.role)
              : "Account workspace"
      }
    >
      {content}
    </AccountLayout>
  );
};
const AccountWorkspace = ({
  management = false,
  onPreviewClose,
  onAccountChanged,
  embedded = false,
}: {
  management?: boolean;
  onPreviewClose?: () => void;
  onAccountChanged?: () => void;
  embedded?: boolean;
}) => {
  const { accountId = "" } = useParams();
  const generation = useFleetStore((s) => s.auth.sessionGeneration);
  const directory = useHostedAccounts();
  const permissions = usePermissions();
  const host = permissions.length > 0 || !!directory?.hostAccess;
  if (management && !host) return <Navigate to={accountPath(accountId)} replace />;
  return (
    <AccountPage
      key={`${accountId}:${generation}:${host}:${management}`}
      accountId={accountId}
      management={management}
      onPreviewClose={onPreviewClose}
      onAccountChanged={onAccountChanged}
      embedded={embedded}
    />
  );
};
export default AccountWorkspace;
