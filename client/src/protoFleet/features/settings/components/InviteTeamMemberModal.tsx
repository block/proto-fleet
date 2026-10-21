import { useCallback, useEffect, useMemo, useState } from "react";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { useTeamInvitations } from "@/protoFleet/api/useTeamInvitations";
import { ShareInvitationModal } from "@/protoFleet/features/hostedAccounts/components/ShareInvitationModal";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface InviteTeamMemberModalProps {
  open?: boolean;
  onDismiss: () => void;
  onCreated: () => void;
}

const expiryOptions = [
  { value: "3600", label: "1 hour" },
  { value: "86400", label: "24 hours" },
];
const defaultExpiry = "86400";

const InviteTeamMemberModal = ({ open, onDismiss, onCreated }: InviteTeamMemberModalProps) => {
  const isVisible = open ?? true;
  const { createTeamInvitation } = useTeamInvitations();
  const { listRoles } = useRoleManagement();
  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [roleId, setRoleId] = useState("");
  const [expiry, setExpiry] = useState(defaultExpiry);
  const [token, setToken] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  // Create stays disabled until roles load; the server still rejects roles
  // the caller cannot assign.
  const [isLoadingRoles, setIsLoadingRoles] = useState(true);
  const [rolesError, setRolesError] = useState<string | null>(null);

  // SUPER_ADMIN is excluded — ownership transfer is a separate flow.
  useEffect(() => {
    if (!isVisible) return;
    let cancelled = false;
    listRoles({
      onSuccess: (roleList) => {
        if (cancelled) return;
        setRoles(roleList.filter((role) => role.builtinKey !== "SUPER_ADMIN"));
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

  const roleOptions = useMemo(
    () => roles.map((role) => ({ value: role.roleId, label: formatRole(role.name), description: role.description })),
    [roles],
  );

  // Reset on every visibility transition so a code never survives a reopen.
  const [prevVisible, setPrevVisible] = useState(isVisible);
  if (prevVisible !== isVisible) {
    setPrevVisible(isVisible);
    if (!isVisible) {
      setRoleId("");
      setExpiry(defaultExpiry);
      setToken(null);
      setIsSubmitting(false);
      setErrorMsg("");
    }
    setIsLoadingRoles(true);
    setRolesError(null);
  }

  const handleCreate = useCallback(() => {
    if (!roleId) {
      setErrorMsg("Select a role before creating the invitation");
      return;
    }
    setIsSubmitting(true);
    setErrorMsg("");
    createTeamInvitation({
      roleId,
      expiresInSeconds: Number(expiry),
      onSuccess: (code) => {
        setToken(code);
        onCreated();
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to create invitation. Please try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }, [createTeamInvitation, expiry, onCreated, roleId]);

  if (isVisible && token) {
    return <ShareInvitationModal token={token} onDismiss={onDismiss} />;
  }

  const canCreate = !isLoadingRoles && !rolesError && Boolean(roleId);
  return (
    <Modal
      open={isVisible}
      // Closing mid-request would hide a code the server already issued.
      onDismiss={isSubmitting ? undefined : onDismiss}
      title="Invite team member"
      buttons={[
        {
          text: "Create invitation",
          onClick: handleCreate,
          variant: variants.primary,
          loading: isSubmitting,
          disabled: !canCreate,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
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
        <div className="flex flex-col gap-2">
          <Select
            id="invite-role"
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
        <Select id="invite-expiry" label="Expires after" value={expiry} options={expiryOptions} onChange={setExpiry} />
        <p className="text-200 text-text-primary-50">
          You’ll receive a single-use invitation code to share. They’ll choose their own login.
        </p>
      </div>
    </Modal>
  );
};

export default InviteTeamMemberModal;
