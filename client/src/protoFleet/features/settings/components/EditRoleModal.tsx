import { useCallback, useEffect, useMemo, useState } from "react";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface EditRoleModalProps {
  open?: boolean;
  userId: string;
  username: string;
  /**
   * Role name from `UserInfo.role` (the server role's `name` column —
   * e.g. "ADMIN", "FIELD_TECH", "SUPER_ADMIN", or a custom role name).
   * Matched against `RoleItem.name` from `listRoles` to preselect the
   * picker; `role_id` is not on `UserInfo` so the match is by name.
   *
   * Do NOT pass the display label (e.g. "Admin", "Field Tech") here —
   * those come from `formatRole()` and don't match the server name.
   */
  currentRoleName: string;
  onDismiss: () => void;
  onSuccess: () => void;
}

const EditRoleModal = ({ open, userId, username, currentRoleName, onDismiss, onSuccess }: EditRoleModalProps) => {
  const isVisible = open ?? true;
  const { updateUserRole } = useUserManagement();
  const { listRoles } = useRoleManagement();

  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [roleId, setRoleId] = useState("");
  const [isLoadingRoles, setIsLoadingRoles] = useState(true);
  const [rolesError, setRolesError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  // Reset state on every visibility transition — the modal is always mounted,
  // so without this a previous user's role id leaks into the next open.
  const [prevVisible, setPrevVisible] = useState(isVisible);
  if (prevVisible !== isVisible) {
    setPrevVisible(isVisible);
    setIsLoadingRoles(true);
    setRolesError(null);
    setRoles([]);
    setRoleId("");
    setErrorMsg("");
    setIsSubmitting(false);
  }

  // Load assignable roles when the modal opens. SUPER_ADMIN is excluded —
  // ownership transfer is a separate flow.
  useEffect(() => {
    if (!isVisible) return;
    let cancelled = false;
    listRoles({
      onSuccess: (roleList) => {
        if (cancelled) return;
        const assignable = roleList.filter((role) => role.builtinKey !== "SUPER_ADMIN");
        setRoles(assignable);
        const current = assignable.find((role) => role.name === currentRoleName);
        setRoleId(current?.roleId ?? "");
      },
      onError: (error) => {
        if (cancelled) return;
        const message = error || "Failed to load roles";
        setRolesError(message);
        pushToast({ message, status: STATUSES.error });
      },
      onFinally: () => {
        if (cancelled) return;
        setIsLoadingRoles(false);
      },
    });
    return () => {
      cancelled = true;
    };
  }, [isVisible, listRoles, currentRoleName]);

  // Display the formatted label ("Admin"/"Field Tech"); preselect still
  // matches the canonical `role.name` against `currentRoleName` from the
  // wire.
  const roleOptions = useMemo(
    () => roles.map((role) => ({ value: role.roleId, label: formatRole(role.name), description: role.description })),
    [roles],
  );

  // Disable Save when the picker matches the current role so the "nothing
  // to save" state is legible (the server also no-ops the swap).
  const currentRole = useMemo(() => roles.find((role) => role.name === currentRoleName), [roles, currentRoleName]);
  const isDirty = roleId !== "" && roleId !== currentRole?.roleId;
  const canSave = !isLoadingRoles && !rolesError && isDirty;

  const handleSave = useCallback(() => {
    if (!roleId) {
      setErrorMsg("Select a role before saving");
      return;
    }
    setIsSubmitting(true);
    setErrorMsg("");
    updateUserRole({
      userId,
      roleId,
      onSuccess: () => {
        pushToast({
          message: `Role updated for ${username}`,
          status: STATUSES.success,
        });
        onSuccess();
        onDismiss();
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to update role. Please try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }, [roleId, userId, username, updateUserRole, onSuccess, onDismiss]);

  return (
    <Modal
      open={isVisible}
      onDismiss={onDismiss}
      title={`Edit role for ${username}`}
      buttons={[
        {
          text: "Save",
          onClick: handleSave,
          variant: variants.primary,
          loading: isSubmitting,
          disabled: !canSave,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <div className="mb-6">Pick a new role for this team member. Their permissions update immediately.</div>

      {rolesError ? (
        <Callout
          className="mb-6"
          intent="danger"
          prefixIcon={<Alert />}
          title={`${rolesError} — close and reopen this dialog to try again.`}
        />
      ) : null}

      {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

      <div className="flex flex-col gap-2">
        <Select
          id="role"
          label="Role"
          options={roleOptions}
          value={roleId}
          onChange={(value) => {
            setRoleId(value);
            setErrorMsg("");
          }}
          disabled={isLoadingRoles || Boolean(rolesError)}
          forceBelow
        />
        <span className="text-200 text-text-primary-50">
          {isLoadingRoles
            ? "Loading roles…"
            : "The role sets what this member can see and do. Manage roles in Team → Roles."}
        </span>
      </div>
    </Modal>
  );
};

export default EditRoleModal;
