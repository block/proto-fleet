import { type ReactNode } from "react";
import { Navigate } from "react-router-dom";

import { accountPath } from "./capabilities";
import { AccountState } from "./components/AccountState";
import { useHostedAccounts } from "./HostedAccountsContext";
import { usePermissions } from "@/protoFleet/store";

// Membership-only logins must not mount host dashboards, streams or operation
// providers while the directory resolves their available account workspaces.
export const HostWorkspaceGate = ({ children }: { children: ReactNode }) => {
  const permissions = usePermissions();
  const directory = useHostedAccounts();
  if (permissions.length || !directory) return children;
  // A failed background refresh keeps the last directory; only an unresolved
  // directory may replace an already-mounted host workspace with an error.
  if (directory.loading || (directory.error && !directory.hostAccess))
    return <AccountState loading={directory.loading} error={directory.error} retry={directory.refresh} />;
  if (!directory.hostAccess)
    return (
      <Navigate
        to={
          directory.accounts[0]
            ? accountPath(directory.accounts[0].accountId, directory.accounts[0].depth === 0 ? "children" : "overview")
            : "/accounts"
        }
        replace
      />
    );
  return children;
};
