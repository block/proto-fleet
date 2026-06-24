import type { ReactElement } from "react";

import { Power, Stop } from "@/shared/assets/icons";
import { type ButtonVariant, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

export type CurtailmentStopConfirmationAction = "forceRestore" | "restore" | "stopCurtailment";

interface CurtailmentStopConfirmationDialogProps {
  open: boolean;
  action: CurtailmentStopConfirmationAction;
  isSubmitting?: boolean;
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
  if (action === "forceRestore") {
    return {
      title: "Force restore automation event?",
      body: "Force restore overrides active automation demand and minimum-duration guards. Miners will start restoring even if MQTT demand is still OFF or the source is stale.",
      confirmText: "Force restore",
      confirmVariant: variants.secondaryDanger,
      icon: <Power />,
      iconIntent: "critical",
    };
  }

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
  isSubmitting = false,
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
          disabled: isSubmitting,
        },
        {
          text: copy.confirmText,
          variant: copy.confirmVariant,
          onClick: onConfirm,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">{copy.body}</div>
    </Dialog>
  );
}

export default CurtailmentStopConfirmationDialog;
