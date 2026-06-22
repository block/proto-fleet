import { useCallback, useEffect, useMemo, useState } from "react";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import AddTeamMemberModal from "@/protoFleet/features/settings/components/AddTeamMemberModal";
import DeactivateUserDialog from "@/protoFleet/features/settings/components/DeactivateUserDialog";
import EditRoleModal from "@/protoFleet/features/settings/components/EditRoleModal";
import ResetPasswordModal from "@/protoFleet/features/settings/components/ResetPasswordModal";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { useHasPermission, useRole, useUsername } from "@/protoFleet/store";
import { Edit, Lock, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type UserData = {
  userId: string;
  username: string;
  passwordUpdatedAt: Date | null;
  lastLoginAt: Date | null;
  role: string;
  requiresPasswordChange: boolean;
};

type UserColumns = "username" | "passwordUpdatedAt" | "lastLoginAt" | "role";

const colTitles: ColTitles<UserColumns> = {
  username: "Username",
  passwordUpdatedAt: "Password Updated",
  lastLoginAt: "Last Login",
  role: "Role",
};

const Team = () => {
  const { listUsers, resetUserPassword, deactivateUser } = useUserManagement();
  // Gate the team-management UI on user:manage; the server's parity
  // check still bounds which roles a caller can assign at create or
  // update time.
  const canAddTeamMembers = useHasPermission("user:manage", { scope: "org" });
  const currentUsername = useUsername();
  const currentRole = useRole();
  const callerIsOwner = currentRole === "SUPER_ADMIN";
  const [users, setUsers] = useState<UserData[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showAddMemberModal, setShowAddMemberModal] = useState(false);
  const [resetPasswordUser, setResetPasswordUser] = useState<UserData | null>(null);
  const [resetPasswordTemp, setResetPasswordTemp] = useState<string | null>(null);
  const [isResettingPassword, setIsResettingPassword] = useState(false);
  const [deactivateUserData, setDeactivateUserData] = useState<UserData | null>(null);
  const [isDeactivating, setIsDeactivating] = useState(false);
  const [editRoleUser, setEditRoleUser] = useState<UserData | null>(null);

  const fetchUsers = useCallback(() => {
    setIsLoading(true);
    listUsers({
      onSuccess: (userList) => {
        setUsers(userList);
      },
      onError: (error) => {
        pushToast({
          message: error || "Failed to load team members",
          status: STATUSES.error,
        });
      },
      onFinally: () => {
        setIsLoading(false);
      },
    });
  }, [listUsers]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    fetchUsers();
  }, [fetchUsers]);

  const colConfig: ColConfig<UserData, string, UserColumns> = useMemo(
    () => ({
      username: {
        component: (user) => (
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-full bg-core-primary-fill text-base font-semibold text-text-contrast">
              {user.username.charAt(0).toUpperCase()}
            </div>
            <span className="text-emphasis-300">{user.username}</span>
          </div>
        ),
        width: "w-60",
      },
      passwordUpdatedAt: {
        component: (user) => (
          <span>
            {user.passwordUpdatedAt ? formatTimestamp(Math.floor(user.passwordUpdatedAt.getTime() / 1000)) : "Never"}
          </span>
        ),
        width: "w-48",
      },
      lastLoginAt: {
        component: (user) => (
          <span>{user.lastLoginAt ? formatTimestamp(Math.floor(user.lastLoginAt.getTime() / 1000)) : "Never"}</span>
        ),
        width: "w-48",
      },
      role: {
        component: (user) => <span>{formatRole(user.role)}</span>,
        width: "w-40",
      },
    }),
    [],
  );

  const activeCols: UserColumns[] = ["username", "passwordUpdatedAt", "lastLoginAt", "role"];

  const handleAddMemberSuccess = useCallback(() => {
    fetchUsers();
    setShowAddMemberModal(false);
  }, [fetchUsers]);

  const handleResetPassword = useCallback((user: UserData) => {
    // Open confirmation modal (step 1)
    setResetPasswordUser(user);
    setResetPasswordTemp(null);
  }, []);

  const handleResetPasswordConfirm = useCallback(() => {
    if (!resetPasswordUser) return;

    setIsResettingPassword(true);
    resetUserPassword({
      userId: resetPasswordUser.userId,
      onSuccess: (tempPassword) => {
        setResetPasswordTemp(tempPassword);
        pushToast({
          message: `Password reset for ${resetPasswordUser.username}`,
          status: STATUSES.success,
        });
      },
      onError: (error) => {
        pushToast({
          message: error || "Failed to reset password",
          status: STATUSES.error,
        });
        setResetPasswordUser(null);
      },
      onFinally: () => {
        setIsResettingPassword(false);
      },
    });
  }, [resetPasswordUser, resetUserPassword]);

  const handleDeactivateConfirm = useCallback(() => {
    if (!deactivateUserData) return;

    setIsDeactivating(true);
    deactivateUser({
      userId: deactivateUserData.userId,
      onSuccess: () => {
        pushToast({
          message: `${deactivateUserData.username} has been deactivated`,
          status: STATUSES.success,
        });
        setDeactivateUserData(null);
        fetchUsers();
      },
      onError: (error) => {
        pushToast({
          message: error || "Failed to deactivate user",
          status: STATUSES.error,
        });
      },
      onFinally: () => {
        setIsDeactivating(false);
      },
    });
  }, [deactivateUserData, deactivateUser, fetchUsers]);

  const isSelf = useCallback((user: UserData) => user.username === currentUsername, [currentUsername]);

  // Edit role is hidden on self (no self-demotion via this page) and on
  // every SUPER_ADMIN row — ownership transfer is a separate flow.
  const hideEditRoleFor = useCallback((user: UserData) => isSelf(user) || user.role === "SUPER_ADMIN", [isSelf]);

  // Reset Password and Deactivate are hidden on self (the server rejects
  // self-deactivation and self password change belongs in Settings → Auth)
  // and on SUPER_ADMIN rows when the caller isn't also SUPER_ADMIN (server
  // parity check would refuse).
  const hideMemberMutationFor = useCallback(
    (user: UserData) => isSelf(user) || (user.role === "SUPER_ADMIN" && !callerIsOwner),
    [isSelf, callerIsOwner],
  );

  const availableActions = useMemo(() => {
    if (!canAddTeamMembers) {
      return [];
    }

    return [
      {
        title: "Edit role",
        icon: <Edit />,
        actionHandler: (user: UserData) => setEditRoleUser(user),
        hidden: hideEditRoleFor,
      },
      {
        title: "Reset Password",
        icon: <Lock />,
        actionHandler: handleResetPassword,
        hidden: hideMemberMutationFor,
      },
      {
        title: "Deactivate",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: (user: UserData) => setDeactivateUserData(user),
        hidden: hideMemberMutationFor,
      },
    ];
  }, [canAddTeamMembers, hideEditRoleFor, hideMemberMutationFor, handleResetPassword]);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <Header title="Team" titleSize="text-heading-300" />
        {canAddTeamMembers ? (
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Add team member"
            onClick={() => setShowAddMemberModal(true)}
          />
        ) : null}
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading team members...</div>
      ) : (
        <List<UserData, string, UserColumns>
          items={users}
          itemKey="userId"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          total={users.length}
          itemName={{ singular: "member", plural: "members" }}
          noDataElement={
            <div className="py-10 text-center text-text-primary-50">
              No team members found. Add your first member to get started.
            </div>
          }
          actions={availableActions}
        />
      )}

      <AddTeamMemberModal
        open={showAddMemberModal}
        onDismiss={() => setShowAddMemberModal(false)}
        onSuccess={handleAddMemberSuccess}
      />
      <ResetPasswordModal
        open={!!resetPasswordUser}
        username={resetPasswordUser?.username ?? ""}
        temporaryPassword={resetPasswordTemp}
        onConfirm={handleResetPasswordConfirm}
        onDismiss={() => {
          setResetPasswordUser(null);
          setResetPasswordTemp(null);
          fetchUsers();
        }}
        isResetting={isResettingPassword}
      />
      <DeactivateUserDialog
        open={!!deactivateUserData}
        username={deactivateUserData?.username ?? ""}
        onConfirm={handleDeactivateConfirm}
        onDismiss={() => setDeactivateUserData(null)}
        isSubmitting={isDeactivating}
      />
      <EditRoleModal
        open={!!editRoleUser}
        userId={editRoleUser?.userId ?? ""}
        username={editRoleUser?.username ?? ""}
        currentRoleName={editRoleUser?.role ?? ""}
        onDismiss={() => setEditRoleUser(null)}
        onSuccess={() => {
          fetchUsers();
          setEditRoleUser(null);
        }}
      />
    </div>
  );
};

export default Team;
