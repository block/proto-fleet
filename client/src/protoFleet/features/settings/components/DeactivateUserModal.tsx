import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface DeactivateUserModalProps {
  username: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeactivateUserModal = ({
  username,
  onConfirm,
  onDismiss,
  isSubmitting,
}: DeactivateUserModalProps) => {
  return (
    <Modal onDismiss={onDismiss} size="small" showHeader={false}>
      <div className="flex flex-col gap-6 py-6">
        <div className="flex items-start">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-intent-critical-10">
            <Alert />
          </div>
        </div>

        <div>
          <div className="mb-2 text-heading-300 text-text-primary">
            Deactivate member?
          </div>
          <div className="text-300 text-text-primary-70">
            Are you sure you want to deactivate this member ({username})? They
            will be hidden and removed from your account. This action cannot be
            undone.
          </div>
        </div>

        <div className="flex items-center justify-between gap-3">
          <Button
            variant={variants.secondary}
            size={sizes.base}
            onClick={onDismiss}
            text="Cancel"
          />
          <Button
            variant={variants.danger}
            size={sizes.base}
            onClick={onConfirm}
            text={isSubmitting ? "Deactivating..." : "Confirm deactivation"}
            disabled={isSubmitting}
          />
        </div>
      </div>
    </Modal>
  );
};

export default DeactivateUserModal;
