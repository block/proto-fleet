import { useCallback } from "react";
import { Navigate, useNavigate, useParams } from "react-router-dom";

import { AccountLayout } from "./AccountLayout";
import { useRequiredAccountScope } from "./AccountScope";
import { AccountScopeProvider } from "./AccountScopeProvider";
import AccountWorkspace from "./AccountWorkspace";
import { accountManagementPath, accountPath, can } from "./capabilities";
import AccountAssets from "./components/AccountAssets";
import { AccountState } from "./components/AccountState";
import { ClientPermissions } from "./components/ClientPermissions";
import { useHostedAccounts } from "./HostedAccountsContext";
import { useAccountQuery } from "./useAccountQuery";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes } from "@/shared/components/Button";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

const ClientManagementContent = () => {
  const { accountId = "", section = "assets" } = useParams();
  const { account, client } = useRequiredAccountScope();
  const directory = useHostedAccounts();
  const navigate = useNavigate();
  const load = useCallback(
    (signal: AbortSignal) => client.listChildren({ accountId: account.accountId }, { signal }),
    [account.accountId, client],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const child = query.data?.accounts.find((item) => item.accountId === accountId);
  if (!child)
    return (
      <AccountState
        loading={query.loading}
        error={query.error ?? "This client is unavailable."}
        retry={query.refresh}
      />
    );
  const configure = (account.capabilityCeiling & 9n) === 9n;
  const tabs = [
    { id: "assets", label: "Mining assets" },
    ...(can(child, 8n) ? [{ id: "team", label: "Team" }] : []),
    ...(can(child, 1n) ? [{ id: "settings", label: "Client settings" }] : []),
    ...(configure ? [{ id: "permissions", label: "Permissions" }] : []),
  ];
  if (!tabs.some((tab) => tab.id === section))
    return <Navigate to={accountManagementPath(accountId, "assets")} replace />;
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <Breadcrumb segments={[{ label: "Clients", to: "/accounts" }, { label: child.name }]} />
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-heading-300 text-text-primary">{child.name}</h1>
        {can(child, 1n) ? (
          <Button variant="secondary" size={sizes.compact} text="View as client" to={accountPath(accountId)} />
        ) : null}
      </div>
      <div className="overflow-x-auto">
        <TabStrip
          ariaLabel="Client sections"
          activeId={section}
          onSelect={(id) => {
            navigate(accountManagementPath(accountId, id));
          }}
        >
          {tabs.map((tab) => (
            <TabStripItem key={tab.id} id={tab.id} label={tab.label} />
          ))}
        </TabStrip>
      </div>
      <AccountState error={query.error} retry={query.refresh} />
      {section === "assets" ? (
        <>
          <div className="flex justify-end">
            <Button variant="secondary" size={sizes.compact} text="Assign from Fleet" to="/fleet/miners" />
          </div>
          <AccountAssets key={accountId} child={child} />
        </>
      ) : section === "permissions" ? (
        <ClientPermissions
          key={`${child.accountId}:${child.capabilityCeiling}:${child.suspended}`}
          child={child}
          onSaved={() => {
            query.refresh();
            directory?.refresh();
          }}
        />
      ) : (
        <AccountWorkspace management embedded onAccountChanged={query.refresh} />
      )}
    </div>
  );
};

const ClientManagement = () => {
  const { accountId = "" } = useParams();
  const directory = useHostedAccounts();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const root = directory?.accounts.find((account) => account.depth === 0 && can(account, 16n));
  if (directory?.loading) return <AccountState loading retry={directory.refresh} />;
  if (!root) return <Navigate to={directory?.hostAccess ? "/accounts" : accountPath(accountId)} replace />;
  return (
    <AccountLayout items={[]} roleLabel="Host administration">
      <div className="p-10 phone:p-6">
        <AccountScopeProvider
          key={`${root.accountId}:${generation}:${root.capabilityCeiling}:${root.effectiveCapabilities}`}
          account={root}
          refreshAccount={directory!.refresh}
          invalidateAccount={directory!.refresh}
        >
          <ClientManagementContent key={accountId} />
        </AccountScopeProvider>
      </div>
    </AccountLayout>
  );
};
export default ClientManagement;
