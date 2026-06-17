import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface AdjustPartModalProps {
  part: { id: string; name: string; siteName: string; onHand: number; reorderPoint: number; binLocation: string };
  onDismiss: () => void;
  onSuccess: () => void;
}

const REASON_OPTIONS = [
  { value: "received_shipment", label: "Received shipment" },
  { value: "cycle_count", label: "Cycle count" },
  { value: "damaged_scrapped", label: "Damaged/scrapped" },
  { value: "returned_from_repair", label: "Returned from repair" },
  { value: "other", label: "Other" },
];

const AdjustPartModal = ({ part, onDismiss, onSuccess }: AdjustPartModalProps) => {
  const [onHand, setOnHand] = useState(String(part.onHand));
  const [reorderPoint, setReorderPoint] = useState(String(part.reorderPoint));
  const [binLocation, setBinLocation] = useState(part.binLocation);
  const [reason, setReason] = useState("");
  const [notes, setNotes] = useState("");

  const handleSave = useCallback(() => {
    // TODO: wire to API
    onSuccess();
  }, [onSuccess]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={`Adjust: ${part.name}`}
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSave,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <Input id="adjust-site" label="Site" initValue={part.siteName} readOnly />
        <Input id="adjust-bin" label="Bin location" initValue={binLocation} onChange={(v) => setBinLocation(v)} />
        <Input id="adjust-on-hand" label="On hand" initValue={onHand} onChange={(v) => setOnHand(v)} type="number" />
        <Input
          id="adjust-reorder"
          label="Reorder point"
          initValue={reorderPoint}
          onChange={(v) => setReorderPoint(v)}
          type="number"
        />
        <Select id="adjust-reason" label="Reason" options={REASON_OPTIONS} value={reason} onChange={setReason} />
        <Textarea id="adjust-notes" label="Notes" onChange={(v) => setNotes(v)} rows={2} />
      </div>
    </Modal>
  );
};

export default AdjustPartModal;
