import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import NamePreview from "@/shared/components/NamePreview";
import { INACTIVE_PLACEHOLDER } from "@/shared/constants";

const maxWorkerNameLength = 100;

interface UpdateWorkerNameDialogProps {
  open: boolean;
  currentWorkerName?: string;
  onConfirm: (name: string) => void;
  onDismiss: () => void;
}

const UpdateWorkerNameDialog = ({ open, currentWorkerName, onConfirm, onDismiss }: UpdateWorkerNameDialogProps) => {
  const currentName = currentWorkerName?.trim() ?? "";
  const [inputValue, setInputValue] = useState(currentName);
  const [showNoChangesWarning, setShowNoChangesWarning] = useState(false);

  const handleChange = useCallback((value: string) => {
    setInputValue(value);
  }, []);

  const handleSave = useCallback(() => {
    const trimmed = inputValue.trim();

    if (trimmed === "" || trimmed === currentName) {
      setShowNoChangesWarning(true);
      return;
    }

    onConfirm(trimmed);
  }, [currentName, inputValue, onConfirm]);

  const handleContinueWithoutChanges = useCallback(() => {
    setShowNoChangesWarning(false);
    if (currentName === "") {
      return;
    }

    onConfirm(currentName);
  }, [currentName, onConfirm]);

  if (showNoChangesWarning) {
    return (
      <Dialog
        open={open}
        title="You haven't made any changes"
        subtitle="You can continue to retain your existing worker name, or keep editing. Do you want to continue anyway?"
        subtitleSize="text-300"
        subtitleClassName="text-text-primary-70"
        testId="update-worker-name-no-changes-dialog"
        buttons={[
          {
            text: "No, keep editing",
            variant: variants.secondary,
            onClick: () => setShowNoChangesWarning(false),
          },
          {
            text: "Yes, continue",
            variant: variants.primary,
            onClick: handleContinueWithoutChanges,
          },
        ]}
      />
    );
  }

  return (
    <Modal
      open={open}
      title="Update worker name"
      onDismiss={onDismiss}
      divider={false}
      size="large"
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSave,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="mt-6 flex flex-col gap-4">
        <Input
          id="update-worker-name-input"
          label="Worker name"
          initValue={inputValue}
          onChange={handleChange}
          onKeyDown={(key) => {
            if (key === "Enter") handleSave();
          }}
          maxLength={maxWorkerNameLength}
          autoFocus
          testId="update-worker-name-input"
        />
        <p className="text-300 text-text-primary-70">
          This updates the worker name stored in Fleet and reapplies the miner&apos;s current pool settings.
        </p>
        <div className="max-w-[592px]">
          <NamePreview currentName={currentName || INACTIVE_PLACEHOLDER} newName={inputValue} />
        </div>
      </div>
    </Modal>
  );
};

export default UpdateWorkerNameDialog;
