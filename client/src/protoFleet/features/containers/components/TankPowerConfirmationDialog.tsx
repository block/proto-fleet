import type { ReactElement } from "react";

import { getTankPowerDialogCopy } from "./tankPowerDialogCopy";
import { Power } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface TankPowerConfirmationDialogProps {
  open: boolean;
  /** Tank label shown in the copy, e.g. "Tank 1". */
  label: string;
  /** Direction of the pending toggle: true = powering on, false = powering off. */
  turningOn: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Confirmation for a tank power toggle. The tank switch is not a soft mining
 * pause — it drives the PDU feeding the tank, cutting or restoring line power to
 * every module inside it. Because that is a destructive remote action on real
 * hardware, it must be confirmed with copy that spells out the PDU semantics,
 * mirroring the curtailment stop/restore confirmation pattern. Powering off is a
 * danger action; powering on is a primary action.
 */
function TankPowerConfirmationDialog({
  open,
  label,
  turningOn,
  onCancel,
  onConfirm,
}: TankPowerConfirmationDialogProps): ReactElement {
  const copy = getTankPowerDialogCopy(label, turningOn);

  return (
    <Dialog
      open={open}
      title={copy.title}
      onDismiss={onCancel}
      icon={<DialogIcon intent={copy.iconIntent}>{<Power />}</DialogIcon>}
      testId="tank-power-confirm"
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

export default TankPowerConfirmationDialog;
export type { TankPowerConfirmationDialogProps };
