import { useState } from "react";
import ActionBar from "@/protoFleet/components/ActionBar";
import BulkActionConfirmDialog from "@/protoFleet/components/ActionBar/BulkActions/BulkActionConfirmDialog";
import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";

interface AlertListActionBarProps {
  selectedAlerts: string[];
}

const buttonClassName = "bg-grayscale-white-10! text-grayscale-white-90!";

const AlertListActionBar = ({ selectedAlerts }: AlertListActionBarProps) => {
  const [showWarnDialog, setShowWarnDialog] = useState(false);

  const handleArchive = () => {
    // TODO call API
  };

  const handleRebootConfirmation = (setHidden: (hidden: boolean) => void) => {
    setShowWarnDialog(false);
    setHidden(false);
    // TODO call API
  };

  return (
    <ActionBar
      className="absolute right-0 bottom-4 left-0 z-20"
      selectedItems={selectedAlerts}
      renderActions={(numberOfItems, setHidden) => (
        <>
          <ButtonGroup
            variant={groupVariants.rightAligned}
            size={sizes.compact}
            buttons={[
              {
                className: buttonClassName,
                text: "Archive",
                onClick: handleArchive,
                variant: variants.secondary,
              },
              {
                className: buttonClassName,
                text: "Reboot miners",
                onClick: () => {
                  setHidden(true);
                  setShowWarnDialog(true);
                },
                variant: variants.secondary,
              },
            ]}
          />
          <BulkActionConfirmDialog
            actionConfirmation={{
              title: `Reboot ${numberOfItems} miners?`,
              subtitle:
                "These miners will temporarily go offline but will resume hashing automatically after they reboot.",
              confirmAction: {
                title: "Reboot",
                variant: variants.primary,
              },
              testId: "reboot-confirm-button",
            }}
            show={showWarnDialog}
            onConfirmation={() => handleRebootConfirmation(setHidden)}
            onCancel={() => {
              setShowWarnDialog(false);
              setHidden(false);
            }}
            testId="reboot-miners"
          />
        </>
      )}
    />
  );
};

export default AlertListActionBar;
