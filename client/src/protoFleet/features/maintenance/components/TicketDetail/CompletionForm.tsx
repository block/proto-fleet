import { useCallback, useState } from "react";

import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface CompletionFormProps {
  isMinerTicket?: boolean;
  onSubmit: () => void;
  onCancel: () => void;
}

const RESOLUTION_OPTIONS = [
  { value: "repaired", label: "Repaired" },
  { value: "replaced", label: "Replaced" },
  { value: "deferred", label: "Deferred" },
  { value: "unrepairable", label: "Unrepairable" },
  { value: "no_action", label: "No action needed" },
];

const LOCATION_OPTIONS = [
  { value: "on_rack", label: "On-rack" },
  { value: "repair_bench", label: "Repair bench" },
];

const PARTS_OPTIONS = [
  { value: "fan_filter", label: "Fan Filter (120mm)" },
  { value: "hashboard_s21", label: "Hashboard S21" },
  { value: "apw12_psu", label: "APW12 PSU" },
  { value: "control_board_s21", label: "Control Board S21" },
  { value: "thermal_paste", label: "Thermal Paste (tube)" },
  { value: "heatsink_s21", label: "Heatsink S21" },
];

const CompletionForm = ({ isMinerTicket = true, onSubmit, onCancel }: CompletionFormProps) => {
  const [resolution, setResolution] = useState("repaired");
  const [repairLocation, setRepairLocation] = useState("on_rack");
  const [partsUsed, setPartsUsed] = useState("");
  const [notes, setNotes] = useState("");

  const handleSubmit = useCallback(() => {
    onSubmit();
  }, [onSubmit]);

  const submitText = (() => {
    switch (resolution) {
      case "deferred":
        return "Defer ticket";
      case "unrepairable":
        return "Mark unrepairable";
      case "no_action":
        return "Close ticket";
      default:
        return "Complete repair";
    }
  })();

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="resolution"
          label="Mark as"
          options={RESOLUTION_OPTIONS}
          value={resolution}
          onChange={setResolution}
          forceBelow
        />
        {isMinerTicket ? (
          <Select
            id="repair-location"
            label="Repair location"
            options={LOCATION_OPTIONS}
            value={repairLocation}
            onChange={setRepairLocation}
            forceBelow
          />
        ) : <div />}
      </div>

      <Select
        id="parts-used"
        label="Parts used"
        options={PARTS_OPTIONS}
        value={partsUsed}
        onChange={setPartsUsed}
        forceBelow
      />

      <Textarea id="completion-notes" label="Notes (optional)" onChange={(value) => setNotes(value)} rows={3} />

      <div className="flex justify-end gap-3 pt-1">
        <Button text="Cancel" variant={variants.secondary} size={buttonSizes.compact} onClick={onCancel} />
        <Button
          text={submitText}
          variant={variants.primary}
          size={buttonSizes.compact}
          onClick={handleSubmit}
        />
      </div>
    </div>
  );
};

export default CompletionForm;
