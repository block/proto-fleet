import ComponentStatusModalContent from "./ComponentStatusModalContent";
import MinerStatusModalContent from "./MinerStatusModalContent";
import type { StatusModalProps } from "./types";
import { ArrowRight } from "@/shared/assets/icons";
import Modal from "@/shared/components/Modal";

/**
 * Prop-driven StatusModal container that renders miner or component status
 * based on the componentAddress prop
 *
 * @example
 * ```tsx
 * const [componentAddress, setComponentAddress] = useState<ComponentAddress>();
 *
 * <StatusModal
 *   open={isModalOpen}
 *   componentAddress={componentAddress}
 *   getMinerStatus={getMinerStatus}
 *   getComponentStatus={getComponentStatus}
 *   showBackButton={true}
 * />
 * ```
 */
export function StatusModal<TComponentAddress = any>({
  componentAddress,
  getMinerStatus,
  getComponentStatus,
  open,
  showBackButton = true,
}: StatusModalProps<TComponentAddress>) {
  // Try to get component data if componentAddress is provided
  const componentData = componentAddress !== undefined ? getComponentStatus(componentAddress) : undefined;

  // Show component view if we have valid component data, otherwise show miner view
  if (componentData) {
    const showBack = showBackButton && componentData.onNavigateBack;

    return (
      <Modal
        title={componentData.title}
        buttons={componentData.buttons}
        icon={showBack ? <ArrowRight className="rotate-180" /> : undefined}
        iconAriaLabel={showBack ? "Go back" : undefined}
        onIconClick={showBack ? componentData.onNavigateBack : undefined}
        onDismiss={componentData.onDismiss}
        open={open}
      >
        <ComponentStatusModalContent {...componentData.props} />
      </Modal>
    );
  }

  // Fall back to miner status view
  const minerData = getMinerStatus();
  return (
    <Modal title={minerData.title} buttons={minerData.buttons} onDismiss={minerData.onDismiss} open={open}>
      <MinerStatusModalContent {...minerData.props} />
    </Modal>
  );
}
