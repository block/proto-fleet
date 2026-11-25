import { createContext, useContext } from "react";
import type { Account } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export type Directory = { accounts: Account[]; hostAccess: boolean };
type Value = Directory & { reportingAccess: boolean; loading: boolean; error: string | null; refresh: () => void };
export const HostedAccountsContext = createContext<Value | null>(null);
export const useHostedAccounts = () => useContext(HostedAccountsContext);
