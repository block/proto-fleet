import { useCallback } from "react";

import { PERMISSION_CATALOG } from "@/protoFleet/features/settings/utils/permissionCatalog";

// =============================================================================
// RBAC role management API
// =============================================================================
//
// Follows the callback shape of useUserManagement (onSuccess/onError/onFinally)
// so the settings components consume it identically.
//
// TODO(rbac): the AuthzService RPCs that back this hook are not generated yet.
// The proto package authz.v1 currently defines only the Permission /
// PermissionGroup messages (see proto/authz/v1/authz.proto, "future
// AuthzService RPCs"). Once ListRoles / CreateRole / UpdateRole / DeleteRole
// land:
//   1. add `authzClient` to api/clients.ts (createClient(AuthzService, transport))
//   2. replace each placeholder below with the real `authzClient.*` call,
//      wrapped in handleAuthErrors exactly like useUserManagement.
// Until then this hook serves an in-memory catalog-derived dataset so the role
// builder and Team flow are fully exercisable end to end in the client.

/** Stable identifier code uses for a built-in role. Mirrors authz.BuiltinKey on the server. */
export type BuiltinRoleKey = "SUPER_ADMIN" | "ADMIN" | "FIELD_TECH";

// RoleItem is the client model. Swapping useRoleManagement to AuthzService
// requires a pbToRoleItem adapter at the wire boundary — proto Role uses
// permissionKeys (not permissions), a numeric BuiltinKey enum (not the
// string union here), and Timestamp (not Date | null) for updatedAt.
export interface RoleItem {
  roleId: string;
  name: string;
  /** Short admin-facing summary. */
  description: string;
  /** Effective catalog permission keys granted by the role. */
  permissions: string[];
  /** Built-in roles are seeded server-side; SUPER_ADMIN is immutable. */
  builtin: boolean;
  builtinKey?: BuiltinRoleKey;
  /** Number of active members currently assigned this role. */
  memberCount: number;
  updatedAt: Date | null;
}

interface RoleCallbacks {
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListRolesProps extends RoleCallbacks {
  onSuccess?: (roles: RoleItem[]) => void;
}

interface CreateRoleProps extends RoleCallbacks {
  name: string;
  description?: string;
  permissions: string[];
  onSuccess?: (role: RoleItem) => void;
}

interface UpdateRoleProps extends RoleCallbacks {
  roleId: string;
  name: string;
  description?: string;
  permissions: string[];
  onSuccess?: (role: RoleItem) => void;
}

interface DeleteRoleProps extends RoleCallbacks {
  roleId: string;
  onSuccess?: () => void;
}

/** Returns true for roles that may never be edited or deleted (server-side built-ins). */
export const isImmutable = (role: RoleItem): boolean => role.builtin === true;

// --- Placeholder dataset (remove once AuthzService is wired) -----------------

const ALL_KEYS = PERMISSION_CATALOG.map((entry) => entry.key);
// FIELD_TECH: read-only fleet visibility plus blink, logs, and rack management — matches the server's built-in role definition.
const FIELD_TECH_KEYS = [
  "fleet:read",
  "miner:read",
  "miner:blink_led",
  "miner:download_logs",
  "rack:read",
  "rack:manage",
];

let placeholderRoles: RoleItem[] = [
  {
    roleId: "builtin-super-admin",
    name: "Owner",
    description: "Full system access. Immutable.",
    permissions: ALL_KEYS,
    builtin: true,
    builtinKey: "SUPER_ADMIN",
    memberCount: 1,
    updatedAt: null,
  },
  {
    roleId: "builtin-admin",
    name: "Admin",
    description: "Org admin. Editable by an Owner.",
    permissions: ALL_KEYS,
    builtin: true,
    builtinKey: "ADMIN",
    memberCount: 2,
    updatedAt: null,
  },
  {
    roleId: "builtin-field-tech",
    name: "Field Tech",
    description: "Read fleet data, blink the locator LED, download logs, manage racks.",
    permissions: FIELD_TECH_KEYS,
    builtin: true,
    builtinKey: "FIELD_TECH",
    memberCount: 4,
    updatedAt: null,
  },
];

const cloneRoles = () => placeholderRoles.map((role) => ({ ...role, permissions: [...role.permissions] }));

const useRoleManagement = () => {
  const listRoles = useCallback(async ({ onSuccess, onFinally }: ListRolesProps) => {
    // const response = await authzClient.listRoles({});
    onSuccess?.(cloneRoles());
    onFinally?.();
  }, []);

  const createRole = useCallback(
    async ({ name, description, permissions, onSuccess, onError, onFinally }: CreateRoleProps) => {
      const trimmed = name.trim();
      if (!trimmed) {
        onError?.("Role name is required");
        onFinally?.();
        return;
      }
      if (placeholderRoles.some((role) => role.name.toLowerCase() === trimmed.toLowerCase())) {
        onError?.(`A role named "${trimmed}" already exists`);
        onFinally?.();
        return;
      }

      // TODO(rbac): wire description input once the editor surfaces a description field.
      // const response = await authzClient.createCustomRole({ name: trimmed, description, permissions });
      // Map response via pbToRoleItem; map BuiltinKey enum to BuiltinRoleKey string; convert Timestamp to Date.
      const role: RoleItem = {
        roleId: `role-${Date.now()}`,
        name: trimmed,
        description: description?.trim() ?? "",
        permissions: [...permissions],
        builtin: false,
        memberCount: 0,
        updatedAt: new Date(),
      };
      placeholderRoles = [...placeholderRoles, role];
      onSuccess?.(role);
      onFinally?.();
    },
    [],
  );

  const updateRole = useCallback(
    async ({ roleId, name, description, permissions, onSuccess, onError, onFinally }: UpdateRoleProps) => {
      const existing = placeholderRoles.find((role) => role.roleId === roleId);
      if (!existing) {
        onError?.("Role not found");
        onFinally?.();
        return;
      }
      if (existing.builtinKey === "SUPER_ADMIN") {
        onError?.("The Owner role is immutable");
        onFinally?.();
        return;
      }

      // TODO(rbac): wire description input once the editor surfaces a description field.
      // const response = await authzClient.updateCustomRole({ roleId, name, description, permissions });
      // Map response via pbToRoleItem; map BuiltinKey enum to BuiltinRoleKey string; convert Timestamp to Date.
      const updated: RoleItem = {
        ...existing,
        name: name.trim(),
        description: description === undefined ? existing.description : description.trim(),
        permissions: [...permissions],
        updatedAt: new Date(),
      };
      placeholderRoles = placeholderRoles.map((role) => (role.roleId === roleId ? updated : role));
      onSuccess?.(updated);
      onFinally?.();
    },
    [],
  );

  const deleteRole = useCallback(async ({ roleId, onSuccess, onError, onFinally }: DeleteRoleProps) => {
    const existing = placeholderRoles.find((role) => role.roleId === roleId);
    if (!existing) {
      onError?.("Role not found");
      onFinally?.();
      return;
    }
    if (existing.builtin) {
      onError?.("Built-in roles can't be deleted");
      onFinally?.();
      return;
    }
    if (existing.memberCount > 0) {
      onError?.("Reassign the members on this role before deleting it");
      onFinally?.();
      return;
    }

    // await authzClient.deleteCustomRole({ roleId });
    // Map response via pbToRoleItem; map BuiltinKey enum to BuiltinRoleKey string; convert Timestamp to Date.
    placeholderRoles = placeholderRoles.filter((role) => role.roleId !== roleId);
    onSuccess?.();
    onFinally?.();
  }, []);

  return { listRoles, createRole, updateRole, deleteRole };
};

export { useRoleManagement };
