import { useCallback, useState } from "react";

import ManualAddStep, { type ManualAddStepState } from "./ManualAddStep";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import ActionErrorBanner from "@/protoFleet/features/infrastructure/components/ActionErrorBanner";
import type { InfraBuildingOption, InfraDeviceDraft } from "@/protoFleet/features/infrastructure/types";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface AddInfraDeviceModalProps {
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  initialSiteName?: string;
  onDismiss: () => void;
  // Persists the draft; rejection keeps the modal open with the error
  // shown inline. The caller closes the modal on success.
  onSubmit: (device: InfraDeviceDraft) => Promise<void>;
}

const AddInfraDeviceModal = ({
  siteOptions = [],
  buildingOptions = [],
  initialSiteName,
  onDismiss,
  onSubmit,
}: AddInfraDeviceModalProps) => {
  const [canAdd, setCanAdd] = useState(false);
  const [addHandler, setAddHandler] = useState<(() => void) | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const handleManualStateChange = useCallback((state: ManualAddStepState) => {
    setCanAdd(state.canAdd);
    setAddHandler(() => state.addHandler);
  }, []);

  const handleDraft = useCallback(
    (draft: InfraDeviceDraft) => {
      setIsSubmitting(true);
      setActionError(null);
      onSubmit(draft)
        .catch((error: unknown) => {
          setActionError(getErrorMessage(error) || "Failed to add infrastructure device.");
        })
        .finally(() => {
          setIsSubmitting(false);
        });
    },
    [onSubmit],
  );

  // Blocks escape/click-outside/close-icon while the create is in
  // flight so the request's outcome (success close or inline error)
  // isn't lost to a dismissed modal.
  const handleDismiss = useCallback(() => {
    if (isSubmitting) return;
    onDismiss();
  }, [isSubmitting, onDismiss]);

  return (
    <Modal
      open
      onDismiss={handleDismiss}
      title="Add infrastructure device"
      description="Add a single fan or fan group controlled through a drive, bridge, or PLC."
      buttons={[
        {
          text: isSubmitting ? "Adding…" : "Add device",
          variant: variants.primary,
          onClick: () => addHandler?.(),
          disabled: !canAdd || isSubmitting,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        {actionError ? <ActionErrorBanner message={actionError} /> : null}
        <ManualAddStep
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          initialSiteName={initialSiteName}
          onSuccess={handleDraft}
          onStateChange={handleManualStateChange}
        />
      </div>
    </Modal>
  );
};

export default AddInfraDeviceModal;
