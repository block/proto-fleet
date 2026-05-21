import type { ReactElement } from "react";

import { Stop } from "@/shared/assets/icons";
import { type ButtonVariant, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

export type CurtailmentStopConfirmationAction = "restore" | "stopCurtailment";

interface CurtailmentStopConfirmationDialogProps {
  open: boolean;
  action: CurtailmentStopConfirmationAction;
  onCancel: () => void;
  onConfirm: () => void;
}

interface StopDialogCopy {
  title: string;
  body: string;
  confirmText: string;
  confirmVariant: ButtonVariant;
}

function getStopDialogCopy(action: CurtailmentStopConfirmationAction): StopDialogCopy {
  if (action === "restore") {
    return {
      title: "Restore power?",
      body: "Restore will run in configured batches and keep schedules suppressed until every miner is restored.",
      confirmText: "Restore",
      confirmVariant: variants.primary,
    };
  }

  return {
    title: "Stop curtailment?",
    body: "Restore will run in configured batches and keep schedules suppressed until the event leaves restoring.",
    confirmText: "Start restore",
    confirmVariant: variants.danger,
  };
}

function CurtailmentStopConfirmationDialog({
  open,
  action,
  onCancel,
  onConfirm,
}: CurtailmentStopConfirmationDialogProps): ReactElement {
  const copy = getStopDialogCopy(action);

  return (
    <Dialog
      open={open}
      title={copy.title}
      onDismiss={onCancel}
      icon={
        <DialogIcon intent="critical">
          <Stop />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
        },
        {
          text: copy.confirmText,
          variant: copy.confirmVariant,
          onClick: onConfirm,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">{copy.body}</div>
    </Dialog>
  );
}

export default CurtailmentStopConfirmationDialog;
