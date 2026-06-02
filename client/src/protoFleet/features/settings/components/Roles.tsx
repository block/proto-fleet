import { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import CreateEditRoleModal from "@/protoFleet/features/settings/components/CreateEditRoleModal";
import DeleteRoleDialog from "@/protoFleet/features/settings/components/DeleteRoleDialog";
import { useHasPermission } from "@/protoFleet/store";
import { Edit, Lock, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type RoleColumns = "name" | "permissions" | "members" | "updatedAt";

const colTitles: ColTitles<RoleColumns> = {
  name: "Role",
  permissions: "Permissions",
  members: "Members",
  updatedAt: "Updated",
};

const activeCols: RoleColumns[] = ["name", "permissions", "members", "updatedAt"];

const Roles = () => {
  const canManageRoles = useHasPermission("role:manage");
  const { listRoles, deleteRole } = useRoleManagement();
  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [editRole, setEditRole] = useState<RoleItem | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [deleteRoleData, setDeleteRoleData] = useState<RoleItem | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const fetchRoles = useCallback(() => {
    setIsLoading(true);
    listRoles({
      onSuccess: (roleList) => setRoles(roleList),
      onError: (error) => pushToast({ message: error || "Failed to load roles", status: STATUSES.error }),
      onFinally: () => setIsLoading(false),
    });
  }, [listRoles]);

  useEffect(() => {
    if (canManageRoles) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch when permission resolves; setState inside async fetch is the external-sync pattern
      fetchRoles();
    }
  }, [fetchRoles, canManageRoles]);

  const handleEditorSuccess = useCallback(() => {
    fetchRoles();
    setShowCreateModal(false);
    setEditRole(null);
  }, [fetchRoles]);

  const handleEditorDismiss = useCallback(() => {
    setShowCreateModal(false);
    setEditRole(null);
  }, []);

  const handleDeleteConfirm = useCallback(() => {
    if (!deleteRoleData) return;
    setIsDeleting(true);
    deleteRole({
      roleId: deleteRoleData.roleId,
      onSuccess: () => {
        pushToast({ message: `Role "${deleteRoleData.name}" deleted`, status: STATUSES.success });
        setDeleteRoleData(null);
        fetchRoles();
      },
      onError: (error) => pushToast({ message: error || "Failed to delete role", status: STATUSES.error }),
      onFinally: () => setIsDeleting(false),
    });
  }, [deleteRoleData, deleteRole, fetchRoles]);

  const colConfig: ColConfig<RoleItem, string, RoleColumns> = useMemo(
    () => ({
      name: {
        component: (role) => (
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-full bg-core-primary-5 text-text-primary-70">
              {role.builtin ? (
                <Lock />
              ) : (
                <span className="text-300 font-semibold">{role.name.charAt(0).toUpperCase()}</span>
              )}
            </div>
            <div className="flex min-w-0 flex-col">
              <div className="flex items-center gap-2">
                <span className="text-emphasis-300">{role.name}</span>
                {role.builtin ? (
                  <span className="rounded-full bg-core-primary-5 px-2 py-0.5 text-200 text-text-primary-50">
                    Built-in
                  </span>
                ) : null}
              </div>
              <span className="truncate text-200 text-text-primary-50">{role.description}</span>
            </div>
          </div>
        ),
        width: "w-96",
        allowWrap: true,
      },
      permissions: {
        component: (role) => <span>{role.permissions.length}</span>,
        width: "w-32",
      },
      members: {
        component: (role) => <span>{role.memberCount}</span>,
        width: "w-28",
      },
      updatedAt: {
        component: (role) => (
          <span>{role.updatedAt ? formatTimestamp(Math.floor(role.updatedAt.getTime() / 1000)) : "—"}</span>
        ),
        width: "w-40",
      },
    }),
    [],
  );

  const availableActions = useMemo(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        // SUPER_ADMIN (Owner) is immutable; ADMIN/FIELD_TECH and custom roles are editable.
        hidden: (role: RoleItem) => role.builtinKey === "SUPER_ADMIN",
        actionHandler: (role: RoleItem) => setEditRole(role),
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        hidden: (role: RoleItem) => role.builtin,
        disabled: (role: RoleItem) => role.memberCount > 0,
        actionHandler: (role: RoleItem) => setDeleteRoleData(role),
      },
    ],
    [],
  );

  // Redirect users without role:manage — placed after hooks to satisfy rules-of-hooks.
  if (!canManageRoles) {
    return <Navigate to="/settings/general" replace />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <Header
          title="Roles"
          titleSize="text-heading-300"
          subtitle="Define what members can see and do. Assign roles when you add or edit a member."
        />
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Create role"
          onClick={() => setShowCreateModal(true)}
        />
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading roles...</div>
      ) : (
        <List<RoleItem, string, RoleColumns>
          items={roles}
          itemKey="roleId"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          total={roles.length}
          itemName={{ singular: "role", plural: "roles" }}
          noDataElement={
            <div className="py-10 text-center text-text-primary-50">
              No roles yet. Create your first role to control member access.
            </div>
          }
          actions={availableActions}
        />
      )}

      <CreateEditRoleModal
        key={editRole?.roleId ?? "create"}
        open={showCreateModal || !!editRole}
        role={editRole}
        onDismiss={handleEditorDismiss}
        onSuccess={handleEditorSuccess}
      />
      <DeleteRoleDialog
        open={!!deleteRoleData}
        roleName={deleteRoleData?.name ?? ""}
        memberCount={deleteRoleData?.memberCount ?? 0}
        onConfirm={handleDeleteConfirm}
        onDismiss={() => setDeleteRoleData(null)}
        isSubmitting={isDeleting}
      />
    </div>
  );
};

export default Roles;
