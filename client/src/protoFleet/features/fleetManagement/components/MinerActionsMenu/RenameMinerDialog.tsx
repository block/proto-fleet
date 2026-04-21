import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import NamePreview from "@/shared/components/NamePreview";

const maxNameLength = 100;

interface RenameMinerDialogProps {
  open: boolean;
  deviceIdentifier: string;
  currentMinerName?: string;
  onConfirm: (name: string) => void;
  onDismiss: () => void;
}

const RenameMinerDialog = ({
  open,
  deviceIdentifier,
  currentMinerName,
  onConfirm,
  onDismiss,
}: RenameMinerDialogProps) => {
  const currentName = currentMinerName || deviceIdentifier;
  const [inputValue, setInputValue] = useState(currentName);
  const [showNoChangesWarning, setShowNoChangesWarning] = useState(false);

  const handleChange = useCallback((value: string) => {
    setInputValue(value);
  }, []);

  const handleSave = useCallback(() => {
    const trimmed = inputValue.trim();

    if (trimmed === "" || trimmed === currentName.trim()) {
      setShowNoChangesWarning(true);
      return;
    }

    onConfirm(trimmed);
  }, [inputValue, onConfirm, currentName]);

  if (showNoChangesWarning) {
    return (
      <Dialog
        open={open}
        title="You haven't made any changes"
        subtitle="You can continue to retain your existing miner names, or keep editing. Do you want to continue anyway?"
        subtitleSize="text-300"
        subtitleClassName="text-text-primary-70"
        testId="rename-miner-no-changes-dialog"
        buttons={[
          {
            text: "No, keep editing",
            variant: variants.secondary,
            onClick: () => setShowNoChangesWarning(false),
          },
          {
            text: "Yes, continue",
            variant: variants.primary,
            onClick: onDismiss,
          },
        ]}
      />
    );
  }

  return (
    <Modal
      open={open}
      title="Rename miner"
      onDismiss={onDismiss}
      divider={false}
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
          id="rename-miner-input"
          label="Name"
          initValue={inputValue}
          onChange={handleChange}
          onKeyDown={(key) => {
            if (key === "Enter") handleSave();
          }}
          maxLength={maxNameLength}
          autoFocus
          testId="rename-miner-input"
        />
        <div className="max-w-[592px]">
          <NamePreview currentName={currentName} newName={inputValue} />
        </div>
      </div>
    </Modal>
  );
};

export default RenameMinerDialog;
