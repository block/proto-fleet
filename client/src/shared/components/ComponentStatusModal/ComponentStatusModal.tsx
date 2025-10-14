import ComponentErrorRow from "./ComponentErrorRow";
import type { ComponentError } from "./types";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";

interface ComponentStatusModalProps {
  errors: ComponentError[];
  onDismiss: () => void;
}

const ComponentStatusModal = ({
  errors,
  onDismiss,
}: ComponentStatusModalProps) => {
  const buttons = [
    {
      text: "Done",
      variant: variants.primary,
      onClick: onDismiss,
    },
  ];

  return (
    <Modal buttons={buttons} title="Miner status" onDismiss={onDismiss}>
      <div className="space-y-6 py-6">
        {/* Header with icon and summary */}
        <Header
          icon={
            <div className="bg-status-critical-5 flex h-8 w-8 items-center justify-center rounded-lg">
              <Alert className="text-text-critical" width={iconSizes.xLarge} />
            </div>
          }
          title="Multiple issues detected"
          titleSize="text-heading-300"
          subtitle="Repair now to prevent miner from overheating."
          subtitleClassName="text-300 text-text-primary"
        />

        {/* Error list */}
        <div className="-mx-6 max-h-[30vh] overflow-x-visible overflow-y-auto px-6">
          <div className="divide-y divide-border-5">
            {errors.map((error) => (
              <ComponentErrorRow key={error.id} error={error} />
            ))}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default ComponentStatusModal;
