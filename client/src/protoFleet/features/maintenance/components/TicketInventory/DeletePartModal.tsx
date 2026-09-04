import { useState } from "react";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
interface Props {
  partName: string;
  onDismiss: () => void;
  onDelete: () => Promise<boolean>;
}
const DeletePartModal = ({ partName, onDismiss, onDelete }: Props) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    setBusy(true);
    const ok = await onDelete();
    setBusy(false);
    if (ok) onDismiss();
    else setError("Unable to delete part. Release active allocations first.");
  };
  return (
    <Modal
      open
      title={`Delete ${partName}?`}
      description="This action cannot be undone."
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Delete part",
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
export default DeletePartModal;
