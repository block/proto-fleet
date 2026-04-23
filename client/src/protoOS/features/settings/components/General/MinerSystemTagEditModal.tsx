import { FormEvent, useCallback, useEffect, useState } from "react";
import { useSystemTag } from "@/protoOS/api";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";

interface MinerSystemTagEditModalProps {
  open: boolean;
  currentTag: string;
  onDismiss: () => void;
  onSaved: (tag: string) => void;
}

const MinerSystemTagEditModal = ({ open, currentTag, onDismiss, onSaved }: MinerSystemTagEditModalProps) => {
  const [value, setValue] = useState(currentTag);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const { putSystemTag } = useSystemTag();

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync local value with controlled currentTag prop
    setValue(currentTag);
  }, [currentTag]);

  const handleSave = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed || isSubmitting) return;

    setIsSubmitting(true);

    const toast = pushToast({
      message: "Saving miner ID...",
      status: TOAST_STATUSES.loading,
      ttl: false,
    });

    putSystemTag(trimmed, {
      onSuccess: () => {
        setIsSubmitting(false);
        updateToast(toast, {
          message: "Miner ID saved",
          status: TOAST_STATUSES.success,
          ttl: 2000,
        });
        onSaved(trimmed);
      },
      onError: (message) => {
        setIsSubmitting(false);
        updateToast(toast, {
          message: message || "Failed to save miner ID",
          status: TOAST_STATUSES.error,
          ttl: 3000,
        });
      },
    });
  }, [value, isSubmitting, putSystemTag, onSaved]);

  const handleSubmit = useCallback(
    (e: FormEvent) => {
      e.preventDefault();
      handleSave();
    },
    [handleSave],
  );

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSave,
          disabled: !value.trim() || isSubmitting,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
    >
      <form onSubmit={handleSubmit}>
        <div className="mt-4">
          <h3 className="text-heading-200 text-text-primary">Proto Rig identification</h3>
          <p className="mt-1 text-300 text-text-primary-70">
            Enter the serial number or asset tag printed on the device label.
          </p>
          <div className="mt-4">
            <Input
              id="miner-id"
              label="Serial number or asset tag"
              initValue={currentTag}
              onChange={(val) => setValue(val)}
              autoFocus
              testId="miner-id-input"
            />
          </div>
        </div>
      </form>
    </Modal>
  );
};

export default MinerSystemTagEditModal;
