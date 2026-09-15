import Modal from "@/shared/components/Modal";
import ComponentStatusModalContent from "@/shared/components/StatusModal/ComponentStatusModalContent";
import type { ComponentStatusModalProps } from "@/shared/components/StatusModal/types";

const DiagnosticStatusModal = ({
  title,
  onDismiss,
  ...props
}: ComponentStatusModalProps & {
  title: string;
  onDismiss: () => void;
}) => (
  <Modal open title={title} dialogLabel={title} onDismiss={onDismiss}>
    <ComponentStatusModalContent {...props} />
  </Modal>
);

export default DiagnosticStatusModal;
