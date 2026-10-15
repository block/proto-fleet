import { type Account, AccountRole } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export const capabilities = [
  { value: 1n, label: "View account data" },
  { value: 2n, label: "Manage pool configurations" },
  { value: 4n, label: "Change pools" },
  { value: 8n, label: "Manage memberships" },
  { value: 16n, label: "Delegate and recall miners" },
] as const;
export const can = (account: Account | undefined, capability: bigint) =>
  !!account && !account.suspended && (account.effectiveCapabilities & capability) === capability;
export const roleLabel = (role: AccountRole) =>
  ({
    [AccountRole.VIEWER]: "Viewer",
    [AccountRole.POOL_OPERATOR]: "Pool operator",
    [AccountRole.ADMINISTRATOR]: "Account administrator",
    [AccountRole.UNSPECIFIED]: "Ancestor access",
  })[role];
export const roleOptions = [
  { value: String(AccountRole.VIEWER), label: "Viewer", description: "View account data" },
  {
    value: String(AccountRole.POOL_OPERATOR),
    label: "Pool operator",
    description: "View data and manage pools within account permissions",
  },
  {
    value: String(AccountRole.ADMINISTRATOR),
    label: "Account administrator",
    description: "Manage pools, memberships and permitted delegation",
  },
];
export const accountPath = (accountId: string, section = "overview") =>
  `/accounts/${encodeURIComponent(accountId)}/${section}`;
