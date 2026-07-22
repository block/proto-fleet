import { useCallback, useRef, useState } from "react";
import DeliveryPicker from "./DeliveryPicker";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useDeliveryRouting } from "@/protoFleet/features/alerts/api/useDeliveryRouting";
import type { Rule } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface EditDeliveryModalProps {
  open: boolean;
  rule: Rule | null;
  onDismiss: () => void;
}

const EditDeliveryModal = ({ open, rule, onDismiss }: EditDeliveryModalProps) => {
  const { setRuleRouting } = useAlertsContext();
  const routing = useDeliveryRouting();

  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);

  // Each open/close toggle is a new session; a save that resolves after its session ended must not dismiss the current one.
  const sessionRef = useRef(0);

  // Re-seed form state from the rule each time the modal opens.
  const [prevOpen, setPrevOpen] = useState(open);
  if (prevOpen !== open) {
    setPrevOpen(open);
    sessionRef.current += 1;
    routing.reset(rule?.routing ?? null);
    setErrorMsg("");
    setSaving(false);
  }

  const handleSave = useCallback(async () => {
    if (!rule) return;
    const invalid = routing.validate();
    if (invalid) {
      setErrorMsg(invalid);
      return;
    }
    const session = sessionRef.current;
    setSaving(true);
    try {
      await setRuleRouting(rule.id, routing.toRuleRouting());
      pushToast({ message: `Delivery updated: ${rule.name}`, status: STATUSES.success });
      if (sessionRef.current === session) {
        onDismiss();
      }
    } catch (error) {
      pushToast({
        message: getErrorMessage(error, "Failed to update delivery"),
        status: STATUSES.error,
      });
      if (sessionRef.current === session) {
        setSaving(false);
      }
    }
  }, [rule, routing, setRuleRouting, onDismiss]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title="Edit delivery"
      description={
        rule ? `Choose where "${rule.name}" alerts are delivered. History records the alert either way.` : ""
      }
      buttons={[
        {
          text: saving ? "Saving…" : "Save delivery",
          onClick: () => {
            void handleSave();
          },
          variant: variants.primary,
          dismissModalOnClick: false,
          disabled: saving,
        },
      ]}
      divider={false}
    >
      {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

      <DeliveryPicker
        key={routing.sessionKey}
        mode={routing.mode}
        onModeChange={(next) => {
          routing.setMode(next);
          setErrorMsg("");
        }}
        channels={routing.channels}
        channelsLoaded={routing.channelsLoaded}
        selectedIds={routing.selectedIds}
        onToggleChannel={(id) => {
          routing.toggleChannel(id);
          setErrorMsg("");
        }}
      />
    </Modal>
  );
};

export default EditDeliveryModal;
