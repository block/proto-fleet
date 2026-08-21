import { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import { isImmutable, type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import CreateEditRoleModal from "@/protoFleet/features/settings/components/CreateEditRoleModal";
import DeleteRoleDialog from "@/protoFleet/features/settings/components/DeleteRoleDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { useHasPermission } from "@/protoFleet/store";
import { Edit, Lock, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { classNameToSelectors } from "@/shared/utils/cssUtils";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type RoleColumns = "name" | "permissions" | "members" | "updatedAt";

type RolesProps = {
  embedded?: boolean;
  filtersClassName?: string;
};

const colTitles: ColTitles<RoleColumns> = {
  name: "Role",
  permissions: "Permissions",
  members: "Members",
  updatedAt: "Updated",
};

const activeCols: RoleColumns[] = ["name", "permissions", "members", "updatedAt"];
const systemDefaultRoleDescription = "This is a system default role. Built-in roles cannot be edited or deleted.";
const systemDefaultRoleTriggerClassName = "system-default-role-lock-trigger";

const SystemDefaultRoleLockContent = () => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();
  const closeIgnoreSelectors = classNameToSelectors(systemDefaultRoleTriggerClassName);

  return (
    <div
      ref={triggerRef}
      className={`${systemDefaultRoleTriggerClassName} relative flex w-full text-text-primary-50`}
      data-testid="system-role-lock"
    >
      <button
        type="button"
        className="flex h-8 w-8 items-center justify-center hover:cursor-pointer hover:text-text-primary"
        aria-label="System default role"
        aria-expanded={isOpen}
        aria-haspopup="dialog"
        onClick={() => setIsOpen((current) => !current)}
      >
        <Lock />
      </button>
      {isOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.normal}
          offset={8}
          freezePosition
          className="!space-y-0"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={closeIgnoreSelectors}
          testId="system-role-lock-popover"
        >
          <p className="text-300 text-text-primary-70">{systemDefaultRoleDescription}</p>
        </Popover>
      ) : null}
    </div>
  );
};

const SystemDefaultRoleLock = () => (
  <PopoverProvider>
    <SystemDefaultRoleLockContent />
  </PopoverProvider>
);

const CreateRoleButton = ({ onClick, className }: { onClick: () => void; className?: string }) => (
  <Button variant={variants.primary} size={sizes.compact} text="Create role" onClick={onClick} className={className} />
);

const Roles = ({ embedded = false, filtersClassName }: RolesProps) => {
  const canManageRoles = useHasPermission("role:manage");
  const { listRoles, deleteRole } = useRoleManagement();
  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [editRole, setEditRole] = useState<RoleItem | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [createKeyBump, setCreateKeyBump] = useState(0);
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
    setCreateKeyBump((n) => n + 1);
  }, [fetchRoles]);

  const handleEditorDismiss = useCallback(() => {
    setShowCreateModal(false);
    setEditRole(null);
    setCreateKeyBump((n) => n + 1);
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
          <div className="flex min-w-0 flex-col">
            <span className="text-emphasis-300">{role.builtinKey ? formatRole(role.builtinKey) : role.name}</span>
            <span className="truncate text-200 text-text-primary-50">{role.description}</span>
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
        // Built-in roles are immutable server-side; the action is hidden so the UI doesn't expose a path the server will reject.
        hidden: (role: RoleItem) => isImmutable(role),
        actionHandler: (role: RoleItem) => setEditRole(role),
      },
      {
        title: "Delete",
        icon: <Trash />,
        variant: "destructive" as const,
        hidden: (role: RoleItem) => isImmutable(role),
        disabled: (role: RoleItem) => role.memberCount > 0,
        actionHandler: (role: RoleItem) => setDeleteRoleData(role),
      },
    ],
    [],
  );

  // Redirect users without role:manage — placed after hooks to satisfy rules-of-hooks.
  if (!canManageRoles) {
    return embedded ? null : <Navigate to="/settings/network" replace />;
  }

  return (
    <div className="flex flex-col gap-6">
      {!embedded ? (
        <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
          <SettingsPageHeader
            title="Roles"
            description="Define what members can see and do. Assign roles when you add or edit a member."
          />
          <CreateRoleButton onClick={() => setShowCreateModal(true)} className="shrink-0 phone:w-full" />
        </div>
      ) : null}

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
          actionPlaceholder={(role) => (isImmutable(role) ? <SystemDefaultRoleLock /> : null)}
          filtersClassName={filtersClassName}
          stickyFirstColumn={false}
          headerControls={embedded ? <CreateRoleButton onClick={() => setShowCreateModal(true)} /> : undefined}
          noDataElement={
            <SettingsEmptyState title="No roles yet" description="Create your first role to control member access." />
          }
          actions={availableActions}
        />
      )}

      {showCreateModal || editRole ? (
        <CreateEditRoleModal
          key={editRole?.roleId ?? `create-${createKeyBump}`}
          open={showCreateModal || !!editRole}
          role={editRole}
          onDismiss={handleEditorDismiss}
          onSuccess={handleEditorSuccess}
        />
      ) : null}
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
