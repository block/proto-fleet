import type { ContainerFan, ContainerTank } from "./ContainerOverview";
import { toFanComponentStatus } from "./fanStatusModal";
import { toTankComponentStatus } from "./tankStatusModal";
import { variants } from "@/shared/components/Button";
import { StatusModal } from "@/shared/components/StatusModal";
import type { ComponentStatusData, MinerStatusData } from "@/shared/components/StatusModal/types";

/**
 * Address the container glance drills into. Fans and tanks are both wired: a
 * fan opens a leaf component glance (speed / PWM), a tank opens a tank-level
 * summary (module health breakdown + temp/power). "tank" is a first-class
 * component type in the shared StatusModal framework (liquid-cooling icon), so
 * both reuse ComponentStatusModalContent directly.
 */
type ContainerComponentAddress = { kind: "fan"; fanId: string } | { kind: "tank"; tankId: string };

interface ContainerStatusModalProps {
  /** The fan whose (ⓘ) was clicked, or null when no fan glance is open. */
  fan?: ContainerFan | null;
  /** The tank whose (ⓘ) was clicked, or null when no tank glance is open. */
  tank?: ContainerTank | null;
  onClose: () => void;
}

/**
 * Container-side wrapper around the shared StatusModal container, mirroring
 * ProtoFleetStatusModal but for infrastructure components rather than miners.
 * A fan card's (ⓘ) opens a component-status glance (speed / PWM + running
 * state); a tank card's (ⓘ) opens a tank-level glance (modules
 * healthy/attention/offline + temp/power). Both reuse the exact
 * ComponentStatusModalContent the miner modal drills into — "fan" and "tank"
 * are both first-class component types in that framework. A tank's full
 * drill-down stays the Subtank detail page (card-body click), not this glance.
 *
 * At most one subject is open at a time (the overview owns mutually-exclusive
 * fan/tank selection state). The shell has no miner subject and no parent list
 * to navigate back to, so getMinerStatus is an inert fallback (never reached
 * while a component is selected) and the back button is suppressed.
 * Deliberately NOT routed through ProtoFleetStatusModal, which is welded to a
 * miner deviceId + live-refresh polling + wake-miner; this reuses the shared
 * presentational layer directly, the honest reuse for a non-miner subject.
 */
const ContainerStatusModal = ({ fan, tank, onClose }: ContainerStatusModalProps) => {
  const componentAddress: ContainerComponentAddress | undefined = fan
    ? { kind: "fan", fanId: fan.id }
    : tank
      ? { kind: "tank", tankId: tank.id }
      : undefined;

  const doneButton = { text: "Done", variant: variants.primary, onClick: onClose };

  const getComponentStatus = (address: ContainerComponentAddress): ComponentStatusData | undefined => {
    if (address.kind === "fan") {
      if (!fan) return undefined;
      return { props: toFanComponentStatus(fan), title: "Fan status", buttons: [doneButton], onDismiss: onClose };
    }
    if (!tank) return undefined;
    return { props: toTankComponentStatus(tank), title: "Tank status", buttons: [doneButton], onDismiss: onClose };
  };

  // Never rendered while a component is selected (componentAddress is defined),
  // but the shared container's contract requires it.
  const getMinerStatus = (): MinerStatusData => ({
    props: {
      title: "",
      errors: { hashboard: [], psu: [], fan: [], controlBoard: [], other: [] },
    },
    title: "",
    buttons: [],
    onDismiss: onClose,
  });

  return (
    <StatusModal<ContainerComponentAddress>
      open={componentAddress !== undefined}
      componentAddress={componentAddress}
      getComponentStatus={getComponentStatus}
      getMinerStatus={getMinerStatus}
      showBackButton={false}
    />
  );
};

export default ContainerStatusModal;
export type { ContainerStatusModalProps };
