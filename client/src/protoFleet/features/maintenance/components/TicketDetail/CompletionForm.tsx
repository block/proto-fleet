import { useCallback, useState } from "react";

import { mockInventoryParts } from "../../mockData";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Radio from "@/shared/components/Radio";
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

const CompletionForm = ({ isMinerTicket = true, onSubmit, onCancel }: CompletionFormProps) => {
  const [resolution, setResolution] = useState("");
  const [repairLocation, setRepairLocation] = useState("on_rack");
  const [selectedParts, setSelectedParts] = useState<Set<string>>(new Set());
  const [notes, setNotes] = useState("");

  const handleSubmit = useCallback(() => {
    onSubmit();
  }, [onSubmit]);

  const togglePart = useCallback((partId: string) => {
    setSelectedParts((prev) => {
      const next = new Set(prev);
      if (next.has(partId)) next.delete(partId);
      else next.add(partId);
      return next;
    });
  }, []);

  const availableParts = mockInventoryParts.filter((p) => p.onHand - p.allocated > 0);

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
    <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-4">
      <span className="text-emphasis-300 font-medium">Complete Repair</span>

      <Select
        id="resolution"
        label="Resolution"
        options={RESOLUTION_OPTIONS}
        value={resolution}
        onChange={setResolution}
      />

      {isMinerTicket && (
        <div className="flex flex-col gap-2">
          <span className="text-300 text-text-primary-70">Repair location</span>
          <div className="flex gap-4">
            <label className="flex items-center gap-2 text-300">
              <Radio
                selected={repairLocation === "on_rack"}
                onChange={() => setRepairLocation("on_rack")}
                name="repairLocation"
                value="on_rack"
              />
              On-rack
            </label>
            <label className="flex items-center gap-2 text-300">
              <Radio
                selected={repairLocation === "repair_bench"}
                onChange={() => setRepairLocation("repair_bench")}
                name="repairLocation"
                value="repair_bench"
              />
              Repair bench
            </label>
          </div>
        </div>
      )}

      <div className="flex flex-col gap-2">
        <span className="text-300 text-text-primary-70">Parts used</span>
        <div className="flex max-h-40 flex-col gap-1 overflow-y-auto rounded-lg border border-border-5 p-2">
          {availableParts.length === 0 ? (
            <span className="py-2 text-center text-300 text-text-primary-70">No parts available</span>
          ) : (
            availableParts.map((part) => (
              <label
                key={part.id}
                className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-300 hover:bg-surface-base"
              >
                <Checkbox
                  checked={selectedParts.has(part.id)}
                  onChange={() => togglePart(part.id)}
                />
                <span className="flex-1">{part.name}</span>
                <span className="text-200 text-text-primary-70">
                  {part.onHand - part.allocated} avail
                </span>
              </label>
            ))
          )}
        </div>
        {selectedParts.size > 0 && (
          <span className="text-200 text-text-primary-70">{selectedParts.size} part{selectedParts.size > 1 ? "s" : ""} selected</span>
        )}
      </div>

      <Textarea id="completion-notes" label="Notes" onChange={(value) => setNotes(value)} />

      <div className="flex justify-end gap-3">
        <Button text="Cancel" variant={variants.secondary} size={buttonSizes.compact} onClick={onCancel} />
        <Button
          text={submitText}
          variant={variants.primary}
          size={buttonSizes.compact}
          onClick={handleSubmit}
          disabled={!resolution}
        />
      </div>
    </div>
  );
};

export default CompletionForm;
