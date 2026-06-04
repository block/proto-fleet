import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface DeleteRoleDialogProps {
  open?: boolean;
  roleName: string;
  memberCount: number;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeleteRoleDialog = ({
  open,
  roleName,
  memberCount,
  onConfirm,
  onDismiss,
  isSubmitting,
}: DeleteRoleDialogProps) => {
  const hasMembers = memberCount > 0;

  return (
    <Dialog
      open={open}
      title="Delete role?"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          onClick: onDismiss,
          variant: variants.secondary,
        },
        {
          text: "Delete role",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
          disabled: hasMembers,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        {hasMembers ? (
          <>
            <span className="text-text-primary">{roleName}</span> is assigned to {memberCount}{" "}
            {memberCount === 1 ? "member" : "members"}. Reassign {memberCount === 1 ? "them" : "those members"} to
            another role before deleting it.
          </>
        ) : (
          <>
            Are you sure you want to delete <span className="text-text-primary">{roleName}</span>? This action cannot be
            undone.
          </>
        )}
      </div>
    </Dialog>
  );
};

export default DeleteRoleDialog;
