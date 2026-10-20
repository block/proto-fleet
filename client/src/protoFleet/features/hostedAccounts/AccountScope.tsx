import { createContext, useContext } from "react";

import type { createAccountClient } from "./api";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export type AccountScope = {
  account: Account;
  client: ReturnType<typeof createAccountClient>;
  signal: AbortSignal;
  readOnly?: boolean;
  // A denied mutation may be a failed password confirmation; recheck before
  // replacing an authorized page. Denied private reads invalidate immediately.
  refreshAccount: () => void;
  invalidateAccount: () => void;
};
export const AccountScopeContext = createContext<AccountScope | null>(null);
export const useAccountScope = () => useContext(AccountScopeContext);
export const useRequiredAccountScope = () => {
  const scope = useAccountScope();
  if (!scope) throw new Error("Account page requires an account scope");
  return scope;
};
