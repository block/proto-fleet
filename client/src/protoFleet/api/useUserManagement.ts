import { useCallback } from "react";

import { authClient } from "@/protoFleet/api/clients";
import type {
  CreateUserRequest,
  DeactivateUserRequest,
  ResetUserPasswordRequest,
} from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface CreateUserProps {
  username: CreateUserRequest["username"];
  // roleId is the role to assign on creation. Optional — when omitted the
  // server applies the org's default role (currently ADMIN). When set, the
  // server validates that the role belongs to the caller's org and is not
  // SUPER_ADMIN before assigning.
  roleId?: CreateUserRequest["roleId"];
  onSuccess?: (userId: string, username: string, tempPassword: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListUsersProps {
  onSuccess?: (
    users: Array<{
      userId: string;
      username: string;
      passwordUpdatedAt: Date | null;
      lastLoginAt: Date | null;
      role: string;
      requiresPasswordChange: boolean;
    }>,
  ) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ResetUserPasswordProps {
  userId: ResetUserPasswordRequest["userId"];
  onSuccess?: (tempPassword: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeactivateUserProps {
  userId: DeactivateUserRequest["userId"];
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useUserManagement = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createUser = useCallback(
    async ({ username, roleId, onSuccess, onError, onFinally }: CreateUserProps) => {
      // TODO(rbac): drop this guard once useRoleManagement swaps to
      // AuthzService.ListRoles. The placeholder hook emits non-numeric
      // role ids ("builtin-field-tech", "role-<ts>") that the server's
      // int64 parser rejects; strip them so the server applies the
      // default role until real ids are on the wire.
      const wireRoleId = roleId && /^[1-9][0-9]*$/.test(roleId) ? roleId : undefined;
      await authClient
        .createUser({ username, roleId: wireRoleId })
        .then((response) => {
          onSuccess?.(response.userId, response.username, response.temporaryPassword);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const listUsers = useCallback(
    async ({ onSuccess, onError, onFinally }: ListUsersProps) => {
      await authClient
        .listUsers({})
        .then((response) => {
          const users = response.users.map((user) => ({
            userId: user.userId,
            username: user.username,
            passwordUpdatedAt:
              user.passwordUpdatedAt && user.passwordUpdatedAt.seconds > 0
                ? new Date(Number(user.passwordUpdatedAt.seconds) * 1000)
                : null,
            lastLoginAt:
              user.lastLoginAt && user.lastLoginAt.seconds > 0
                ? new Date(Number(user.lastLoginAt.seconds) * 1000)
                : null,
            role: user.role,
            requiresPasswordChange: user.requiresPasswordChange,
          }));
          onSuccess?.(users);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const resetUserPassword = useCallback(
    async ({ userId, onSuccess, onError, onFinally }: ResetUserPasswordProps) => {
      await authClient
        .resetUserPassword({ userId })
        .then((response) => {
          onSuccess?.(response.temporaryPassword);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const deactivateUser = useCallback(
    async ({ userId, onSuccess, onError, onFinally }: DeactivateUserProps) => {
      await authClient
        .deactivateUser({ userId })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  return {
    createUser,
    listUsers,
    resetUserPassword,
    deactivateUser,
  };
};

export type UseUserManagementReturn = ReturnType<typeof useUserManagement>;

export { useUserManagement };
