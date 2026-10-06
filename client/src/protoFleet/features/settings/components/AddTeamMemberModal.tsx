import { useCallback, useEffect, useMemo, useState } from "react";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { Alert, Copy, Success } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

interface AddTeamMemberModalProps {
  open?: boolean;
  onDismiss: () => void;
  onSuccess: () => void;
}

type ModalStep = "enterUsername" | "displayPassword";

const AddTeamMemberModal = ({ open, onDismiss, onSuccess }: AddTeamMemberModalProps) => {
  const isVisible = open ?? true;
  const { createUser } = useUserManagement();
  const { listRoles } = useRoleManagement();
  const [step, setStep] = useState<ModalStep>("enterUsername");
  const [username, setUsername] = useState("");
  const [temporaryPassword, setTemporaryPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [roleId, setRoleId] = useState("");
  // Track listRoles loading/error so the Save button can fail closed. The
  // server rejects an empty role_id with InvalidArgument, but failing closed
  // here surfaces the error inline instead of as a generic toast.
  const [isLoadingRoles, setIsLoadingRoles] = useState(true);
  const [rolesError, setRolesError] = useState<string | null>(null);

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
  }, [isVisible, listRoles]);

  // `role.name` is the server's seed identifier ("ADMIN"/"FIELD_TECH"/...).
  // Surface the formatted display label to the user; matching elsewhere
  // (e.g. EditRoleModal preselect) still goes through `role.name`.
  const roleOptions = useMemo(
    () => roles.map((role) => ({ value: role.roleId, label: formatRole(role.name), description: role.description })),
    [roles],
  );

  // Reset form state on every visibility transition. Re-arm the loading
  // gate on both open and close so a stale "loaded" flag from a prior
  // session can't briefly enable Save before roles refetch.
  const [prevVisible, setPrevVisible] = useState(isVisible);
  if (prevVisible !== isVisible) {
    setPrevVisible(isVisible);
    if (!isVisible) {
      setStep("enterUsername");
      setUsername("");
      setTemporaryPassword("");
      setIsSubmitting(false);
      setErrorMsg("");
      setRoleId("");
    }
    // Both open and close need the loading gate re-armed and any prior error cleared.
    setIsLoadingRoles(true);
    setRolesError(null);
  }

  const handleCreateUser = useCallback(() => {
    if (!username.trim()) {
      setErrorMsg("Username is required");
      return;
    }
    // Save is disabled until a role is selected, but surface a clear inline
    // message if it ever fires with an empty selection so users don't see the
    // server's generic InvalidArgument toast.
    if (!roleId) {
      setErrorMsg("Select a role before saving");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg("");

    createUser({
      username: username.trim(),
      roleId,
      onSuccess: (_userId, _username, tempPassword) => {
        setTemporaryPassword(tempPassword);
        setStep("displayPassword");
        pushToast({
          message: `Team member ${username} created successfully`,
          status: STATUSES.success,
        });
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to create user. Please try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }, [username, roleId, createUser]);

  const handleCopyPassword = useCallback(() => {
    copyToClipboard(temporaryPassword)
      .then(() => {
        pushToast({
          message: "Password copied to clipboard",
          status: STATUSES.success,
        });
      })
      .catch(() => {
        pushToast({
          message: "Failed to copy password",
          status: STATUSES.error,
        });
      });
  }, [temporaryPassword]);

  const handleDone = useCallback(() => {
    onSuccess();
    onDismiss();
  }, [onSuccess, onDismiss]);

  if (step === "enterUsername") {
    // Fail closed: Save stays disabled until roles load and one is selected.
    // If listRoles failed, Save remains disabled and an inline error explains why.
    const canSave = !isLoadingRoles && !rolesError && Boolean(roleId);
    return (
      <Modal
        open={isVisible}
        onDismiss={onDismiss}
        title="Add team member"
        buttons={[
          {
            text: "Save",
            onClick: handleCreateUser,
            variant: variants.primary,
            loading: isSubmitting,
            disabled: !canSave,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
      >
        <div className="mb-6">
          Add a member by entering their username and choosing a role. Fleet generates a temporary password for you to
          share so they can log in and set a new one.
        </div>

        {rolesError ? (
          <Callout
            className="mb-6"
            intent="danger"
            prefixIcon={<Alert />}
            title={`${rolesError} — close and reopen this dialog to try again.`}
          />
        ) : null}

        {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

        <div className="flex flex-col gap-4">
          <Input
            id="username"
            label="Username"
            initValue={username}
            onChange={(value) => {
              setUsername(value);
              setErrorMsg("");
            }}
            autoFocus
          />

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
              // Open downward so the modal footer doesn't clip the listbox.
              forceBelow
            />
            <span className="text-200 text-text-primary-50">
              {isLoadingRoles
                ? "Loading roles…"
                : "The role sets what this member can see and do. Manage roles in Team → Roles."}
            </span>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Dialog
      open={isVisible}
      testId="modal"
      title="Member added"
      subtitle="Save this password and share it with the user securely. It won't be shown again."
      subtitleSize="text-300"
      onDismiss={handleDone}
      icon={
        <DialogIcon intent="success">
          <Success />
        </DialogIcon>
      }
      buttonGroupVariant={groupVariants.rightAligned}
      buttons={[
        {
          text: "Done",
          onClick: handleDone,
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
        <div className="font-mono text-300 break-all text-text-primary" data-testid="temporary-password">
          {temporaryPassword}
        </div>
        <Button variant="ghost" onClick={handleCopyPassword} ariaLabel="Copy password" prefixIcon={<Copy />} />
      </div>
    </Dialog>
  );
};

export default AddTeamMemberModal;
