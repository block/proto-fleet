import { useState } from "react";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface DeleteTicketModalProps {
  ticketNumber: string;
  onDismiss: () => void;
  onDelete: () => Promise<boolean>;
}

const DeleteTicketModal = ({ ticketNumber, onDismiss, onDelete }: DeleteTicketModalProps) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setBusy(true);
    setError(null);
    const deleted = await onDelete();
    setBusy(false);
    if (deleted) onDismiss();
    else setError("Unable to delete ticket. Refresh the queue and try again.");
  };

  return (
    <Modal
      open
      title={`Delete ${ticketNumber}?`}
      description="This action cannot be undone. Reserved parts will be returned to inventory."
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Delete ticket",
          variant: variants.danger,
          loading: busy,
          onClick: () => void submit(),
          dismissModalOnClick: false,
        },
      ]}
    >
      {error ? <div role="alert">{error}</div> : null}
    </Modal>
  );
};

export default DeleteTicketModal;
