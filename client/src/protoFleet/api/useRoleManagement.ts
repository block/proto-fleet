import { useCallback } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { authzClient } from "@/protoFleet/api/clients";
import { BuiltinKey, type Role } from "@/protoFleet/api/generated/authz/v1/authz_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

/** Stable identifier code uses for a built-in role. Mirrors authz.BuiltinKey on the server. */
export type BuiltinRoleKey = "SUPER_ADMIN" | "ADMIN" | "FIELD_TECH";

export interface RoleItem {
  roleId: string;
  name: string;
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
  description: string;
  permissions: string[];
  onSuccess?: (role: RoleItem) => void;
}

interface UpdateRoleProps extends RoleCallbacks {
  roleId: string;
  name: string;
  description: string;
  permissions: string[];
  onSuccess?: (role: RoleItem) => void;
}

interface DeleteRoleProps extends RoleCallbacks {
  roleId: string;
  onSuccess?: () => void;
}

/** Returns true for roles that may never be edited or deleted (server-side built-ins). */
export const isImmutable = (role: RoleItem): boolean => role.builtin === true;

const builtinKeyToString = (key: BuiltinKey): BuiltinRoleKey | undefined => {
  switch (key) {
    case BuiltinKey.SUPER_ADMIN:
      return "SUPER_ADMIN";
    case BuiltinKey.ADMIN:
      return "ADMIN";
    case BuiltinKey.FIELD_TECH:
      return "FIELD_TECH";
    default:
      return undefined;
  }
};

const pbToRoleItem = (pb: Role): RoleItem => ({
  roleId: pb.roleId,
  name: pb.name,
  description: pb.description,
  permissions: pb.permissionKeys,
  builtin: pb.builtin,
  builtinKey: builtinKeyToString(pb.builtinKey),
  memberCount: pb.memberCount,
  updatedAt: pb.updatedAt ? timestampDate(pb.updatedAt) : null,
});

const useRoleManagement = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listRoles = useCallback(
    async ({ onSuccess, onError, onFinally }: ListRolesProps) => {
      await authzClient
        .listRoles({})
        .then((response) => {
          onSuccess?.(response.roles.map(pbToRoleItem));
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => onError?.(getErrorMessage(err)),
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const createRole = useCallback(
    async ({ name, description, permissions, onSuccess, onError, onFinally }: CreateRoleProps) => {
      await authzClient
        .createCustomRole({ name, description, permissionKeys: permissions })
        .then((response) => {
          if (!response.role) {
            onError?.("Server returned no role");
            return;
          }
          onSuccess?.(pbToRoleItem(response.role));
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => onError?.(getErrorMessage(err)),
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const updateRole = useCallback(
    async ({ roleId, name, description, permissions, onSuccess, onError, onFinally }: UpdateRoleProps) => {
      await authzClient
        .updateCustomRole({ roleId, name, description, permissionKeys: permissions })
        .then((response) => {
          if (!response.role) {
            onError?.("Server returned no role");
            return;
          }
          onSuccess?.(pbToRoleItem(response.role));
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => onError?.(getErrorMessage(err)),
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const deleteRole = useCallback(
    async ({ roleId, onSuccess, onError, onFinally }: DeleteRoleProps) => {
      await authzClient
        .deleteCustomRole({ roleId })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => onError?.(getErrorMessage(err)),
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  return { listRoles, createRole, updateRole, deleteRole };
};

export { useRoleManagement };
