import { useState } from "react";
import type { InventoryPartItem, SiteOption } from "../../types";
import { AdjustmentReason } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
interface Props {
  part: InventoryPartItem;
  sites: SiteOption[];
  onDismiss: () => void;
  onSubmit: (value: {
    id: bigint;
    onHand: number;
    reorderPoint: number;
    binLocation: string;
    siteId?: bigint;
    reason: AdjustmentReason;
  }) => Promise<boolean>;
}
const reasons = [
  { value: String(AdjustmentReason.RECEIVED_SHIPMENT), label: "Received shipment" },
  { value: String(AdjustmentReason.CYCLE_COUNT), label: "Cycle count" },
  { value: String(AdjustmentReason.DAMAGED_SCRAPPED), label: "Damaged/scrapped" },
  { value: String(AdjustmentReason.RETURNED_FROM_REPAIR), label: "Returned from repair" },
  { value: String(AdjustmentReason.OTHER), label: "Other" },
];
const AdjustPartModal = ({ part, sites, onDismiss, onSubmit }: Props) => {
  const [onHand, setOnHand] = useState(String(part.onHand));
  const [reorder, setReorder] = useState(String(part.reorderPoint));
  const [bin, setBin] = useState(part.binLocation);
  const [siteId, setSiteId] = useState(part.siteId ?? "");
  const [reason, setReason] = useState(AdjustmentReason.UNSPECIFIED);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const valid =
    Number.isInteger(Number(onHand)) &&
    Number(onHand) >= 0 &&
    Number.isInteger(Number(reorder)) &&
    Number(reorder) >= 0 &&
    reason !== AdjustmentReason.UNSPECIFIED;
  const save = async () => {
    if (!valid) return;
    setBusy(true);
    const ok = await onSubmit({
      id: BigInt(part.id),
      onHand: Number(onHand),
      reorderPoint: Number(reorder),
      binLocation: bin,
      ...(siteId && siteId !== part.siteId ? { siteId: BigInt(siteId) } : {}),
      reason,
    });
    setBusy(false);
    if (ok) onDismiss();
    else setError("Unable to adjust part");
  };
  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={`Adjust: ${part.name}`}
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          disabled: !valid,
          loading: busy,
          onClick: () => void save(),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <Select
          id="adjust-site"
          label="Site"
          options={[
            ...(siteId ? [] : [{ value: "", label: "No site" }]),
            ...sites.map((site) => ({ value: site.id, label: site.name })),
          ]}
          value={siteId}
          onChange={setSiteId}
        />
        <Input id="adjust-bin" label="Bin location" initValue={bin} onChange={setBin} />
        <Input id="adjust-on-hand" label="On hand" initValue={onHand} onChange={setOnHand} type="number" />
        <Input id="adjust-reorder" label="Reorder point" initValue={reorder} onChange={setReorder} type="number" />
        <Select
          id="adjust-reason"
          label="Reason"
          options={reasons}
          value={String(reason)}
          onChange={(value) => setReason(Number(value) as AdjustmentReason)}
        />
        {error ? <div role="alert">{error}</div> : null}
      </div>
    </Modal>
  );
};
export default AdjustPartModal;
