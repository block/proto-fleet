import { type ReactNode, useLayoutEffect, useState } from "react";

import { type AccountScope, AccountScopeContext } from "./AccountScope";
import { createAccountConnection } from "./api";

export const AccountScopeProvider = ({
  account,
  readOnly,
  refreshAccount,
  invalidateAccount,
  children,
}: Omit<AccountScope, "client" | "signal"> & { children: ReactNode }) => {
  const [connection] = useState(() => createAccountConnection(account.accountId, { readOnly }));
  useLayoutEffect(() => {
    connection.activate();
    return () => connection.dispose();
  }, [connection]);
  return (
    <AccountScopeContext.Provider
      value={{
        account,
        readOnly,
        refreshAccount,
        invalidateAccount,
        client: connection.client,
        signal: connection.signal,
      }}
    >
      {children}
    </AccountScopeContext.Provider>
  );
};
