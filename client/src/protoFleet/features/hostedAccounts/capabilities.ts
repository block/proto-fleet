import { type Account, AccountRole } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export const capabilities = [
  { value: 1n, label: "View account data" },
  { value: 8n, label: "Manage memberships" },
  {
    value: 2n,
    label: "Manage pool configurations",
    description: "Create, revise and archive saved pool configurations.",
  },
  {
    value: 4n,
    label: "Select stored pool profile",
    description: "Choose a saved revision; does not apply pools to mining assets.",
  },
  {
    value: 16n,
    label: "Allocate and recall assets",
    description: "Allocate assets to subclients and recall them to this account.",
  },
] as const;
export const apiKeyCapabilities = capabilities.filter(
  (capability) => capability.value === 1n || capability.value === 2n,
);
export const can = (account: Account | undefined, capability: bigint) =>
  !!account && !account.suspended && (account.effectiveCapabilities & capability) === capability;
export const roleLabel = (role: AccountRole) =>
  ({
    [AccountRole.VIEWER]: "Viewer",
    [AccountRole.POOL_OPERATOR]: "Pool operator",
    [AccountRole.ADMINISTRATOR]: "Account administrator",
    [AccountRole.UNSPECIFIED]: "Host and parent access",
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
    description: "Manage pools, memberships and permitted asset allocation",
  },
];
export const accountPath = (accountId: string, section = "overview") =>
  `/accounts/${encodeURIComponent(accountId)}/${section}`;
export const accountManagementPath = (accountId: string, section = "assets") =>
  `/accounts/${encodeURIComponent(accountId)}/manage/${section}`;
