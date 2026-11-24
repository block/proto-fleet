import { type ButtonVariant, variants } from "@/shared/components/Button";

export interface TankPowerDialogCopy {
  title: string;
  body: string;
  confirmText: string;
  confirmVariant: ButtonVariant;
  iconIntent: "success" | "critical";
}

/**
 * Copy for the tank power confirmation, keyed on toggle direction. Pure (no DOM)
 * so the PDU-warning wording — the point of the dialog — can be unit-tested.
 * Both bodies lead with the PDU semantics: the switch cuts/restores line power
 * to every module in the tank, not a soft mining pause.
 */
export function getTankPowerDialogCopy(label: string, turningOn: boolean): TankPowerDialogCopy {
  if (turningOn) {
    return {
      title: `Power on ${label}?`,
      body: `This switch controls the tank's PDU, not a mining pause. Confirming restores line power to every module in ${label}; they will boot and resume hashing.`,
      confirmText: "Power on",
      confirmVariant: variants.primary,
      iconIntent: "success",
    };
  }

  return {
    title: `Power off ${label}?`,
    body: `This switch controls the tank's PDU, not a mining pause. Confirming cuts line power to every module in ${label} — they stop hashing and stay off until the PDU is powered back on.`,
    confirmText: "Power off",
    confirmVariant: variants.danger,
    iconIntent: "critical",
  };
}
