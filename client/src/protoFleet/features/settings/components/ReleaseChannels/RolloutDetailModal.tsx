import RolloutLiveView, { type RolloutLiveViewProps } from "./RolloutLiveView";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

interface RolloutDetailModalProps extends Omit<RolloutLiveViewProps, "presentation" | "onClose"> {
  onClose: () => void;
}

const RolloutDetailModal = (props: RolloutDetailModalProps) => (
  <Modal
    open
    onDismiss={props.onClose}
    size={modalSizes.fullscreen}
    showHeader={false}
    className="!p-0"
    bodyClassName="flex h-full min-h-0 w-full flex-col overflow-auto bg-surface-base"
  >
    <div className="flex w-full flex-1 p-4 tablet:p-6" data-testid={`rollout-detail-${props.rollout.id.toString()}`}>
      <RolloutLiveView {...props} presentation="fullscreen" />
    </div>
  </Modal>
);

export default RolloutDetailModal;
