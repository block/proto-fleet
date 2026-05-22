import type { ReactElement } from "react";

import { Power, Stop } from "@/shared/assets/icons";
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
  icon: ReactElement;
  iconIntent: "critical" | "success";
}

function getStopDialogCopy(action: CurtailmentStopConfirmationAction): StopDialogCopy {
  if (action === "restore") {
    return {
      title: "Restore power?",
      body: "Restore miners in configured batches. Schedules stay suppressed until every miner is restored.",
      confirmText: "Restore power",
      confirmVariant: variants.primary,
      icon: <Power />,
      iconIntent: "success",
    };
  }

  return {
    title: "Stop curtailment?",
    body: "Stop this curtailment and start restoring miners in configured batches. Schedules stay suppressed until the event leaves restoring.",
    confirmText: "Confirm stop",
    confirmVariant: variants.danger,
    icon: <Stop />,
    iconIntent: "critical",
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
      icon={<DialogIcon intent={copy.iconIntent}>{copy.icon}</DialogIcon>}
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
