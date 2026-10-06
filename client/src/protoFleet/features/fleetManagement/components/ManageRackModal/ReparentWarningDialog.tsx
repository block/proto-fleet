import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface ReparentWarningDialogProps {
  /** How many of the miners being assigned are currently placed elsewhere. */
  count: number;
  /** Target rack label, shown in the message. */
  rackLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Confirmation shown before assigning miners that are already in another
 * rack/building/site — assigning them here removes them from their current
 * placement (#672). Shared by the rack edit modal and the rack overview
 * quick-assign flow so the warning copy stays consistent.
 */
export default function ReparentWarningDialog({ count, rackLabel, onCancel, onConfirm }: ReparentWarningDialogProps) {
  const single = count === 1;
  return (
    <Dialog
      title={single ? "Reassign this miner?" : `Reassign ${count} miners?`}
      subtitle={
        single
          ? `This miner is currently assigned to another rack, building, or site. Assigning it to "${rackLabel}" will remove it from its current placement.`
          : `${count} of these miners are currently assigned to another rack, building, or site. Assigning them to "${rackLabel}" will remove them from their current placement.`
      }
      onDismiss={onCancel}
      buttons={[
        { text: "Cancel", onClick: onCancel, variant: variants.secondary },
        { text: "Reassign", onClick: onConfirm, variant: variants.primary },
      ]}
    />
  );
}
