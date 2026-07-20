import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

/** A rack being reparented, with the count of miners that move with it. */
export interface ReparentedRack {
  rackId: bigint;
  label: string;
  minerCount: number;
}

interface RackReparentWarningDialogProps {
  /** The racks being assigned here that are currently placed elsewhere. */
  racks: ReparentedRack[];
  /** Target building name, shown in the message. */
  buildingName: string;
  /** The move commits on confirm; `busy` disables the actions and shows a
   *  progress label on Move while that RPC is in flight. */
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Confirmation shown before assigning racks that already live in another
 * building or site — assigning them here moves the rack, and every miner in it,
 * out of its current placement (#766). Analog of the miner-side
 * `ReparentWarningDialog`; copy explicitly states the rack's miners move with it
 * so the operator understands the blast radius.
 */
export default function RackReparentWarningDialog({
  racks,
  buildingName,
  busy = false,
  onCancel,
  onConfirm,
}: RackReparentWarningDialogProps) {
  const single = racks.length === 1;
  const totalMiners = racks.reduce((sum, r) => sum + r.minerCount, 0);
  const minerPhrase = totalMiners === 1 ? "1 miner" : `${totalMiners} miners`;
  return (
    <Dialog
      title={single ? "Move this rack?" : `Move ${racks.length} racks?`}
      subtitle={
        single
          ? `Rack "${racks[0].label || "(unnamed rack)"}" is currently in another building or site. Moving it to "${buildingName}" will take the rack and its ${minerPhrase} out of its current placement.`
          : `${racks.length} of these racks are currently in another building or site. Moving them to "${buildingName}" will take those racks and their ${minerPhrase} out of their current placement.`
      }
      onDismiss={onCancel}
      buttons={[
        { text: "Cancel", onClick: onCancel, variant: variants.secondary, disabled: busy },
        {
          text: busy ? "Moving…" : "Move",
          onClick: onConfirm,
          variant: variants.primary,
          loading: busy,
          disabled: busy,
        },
      ]}
    />
  );
}
