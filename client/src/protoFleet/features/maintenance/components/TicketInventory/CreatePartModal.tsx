import { useState } from "react";
import type { SiteOption } from "../../types";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

export type CreatePartInput = {
  name: string;
  type: string;
  manufacturer: string;
  partNumber: string;
  siteId?: bigint;
  onHand: number;
  reorderPoint: number;
  binLocation: string;
};
interface Props {
  sites: SiteOption[];
  onDismiss: () => void;
  onSubmit: (input: CreatePartInput) => Promise<boolean>;
}
const CreatePartModal = ({ sites, onDismiss, onSubmit }: Props) => {
  const [name, setName] = useState("");
  const [type, setType] = useState("");
  const [manufacturer, setManufacturer] = useState("");
  const [partNumber, setPartNumber] = useState("");
  const [siteId, setSiteId] = useState("");
  const [onHand, setOnHand] = useState("0");
  const [reorderPoint, setReorderPoint] = useState("0");
  const [binLocation, setBinLocation] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const valid =
    name.trim() &&
    type.trim() &&
    Number.isInteger(Number(onHand)) &&
    Number(onHand) >= 0 &&
    Number.isInteger(Number(reorderPoint)) &&
    Number(reorderPoint) >= 0;
  const submit = async () => {
    if (!valid) return;
    setBusy(true);
    const ok = await onSubmit({
      name,
      type,
      manufacturer,
      partNumber,
      siteId: siteId ? BigInt(siteId) : undefined,
      onHand: Number(onHand),
      reorderPoint: Number(reorderPoint),
      binLocation,
    });
    setBusy(false);
    if (ok) onDismiss();
    else setError("Unable to add part");
  };
  return (
    <Modal
      open
      title="Add part"
      onDismiss={onDismiss}
      buttons={[
        {
          text: "Add part",
          variant: variants.primary,
          disabled: !valid,
          loading: busy,
          onClick: () => void submit(),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="grid grid-cols-2 gap-3">
        <Input id="part-name" label="Part name" onChange={setName} />
        <Input id="part-type" label="Type" onChange={setType} />
        <Input id="manufacturer" label="Manufacturer" onChange={setManufacturer} />
        <Input id="part-number" label="Part number" onChange={setPartNumber} />
        <Select
          id="part-site"
          label="Site"
          options={[{ value: "", label: "No site" }, ...sites.map((site) => ({ value: site.id, label: site.name }))]}
          value={siteId}
          onChange={setSiteId}
        />
        <Input id="part-on-hand" label="On hand" type="number" initValue={onHand} onChange={setOnHand} />
        <Input
          id="part-reorder"
          label="Reorder point"
          type="number"
          initValue={reorderPoint}
          onChange={setReorderPoint}
        />
        <Input id="part-bin" label="Bin location" onChange={setBinLocation} />
        {error ? <div role="alert">{error}</div> : null}
      </div>
    </Modal>
  );
};
export default CreatePartModal;
