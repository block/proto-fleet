import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

interface BulkCloseModalProps {
  ticketIds: string[];
  onDismiss: () => void;
  onSuccess: () => void;
}

const RESOLUTIONS = [
  { value: "repaired", label: "Repaired", description: "Issue was fixed" },
  { value: "replaced", label: "Replaced", description: "Component was swapped out" },
  { value: "no_action", label: "No action needed", description: "Issue resolved itself or was a false positive" },
  { value: "deferred", label: "Deferred", description: "Moved to a future maintenance window" },
  { value: "unrepairable", label: "Unrepairable", description: "Cannot be fixed, needs decommission" },
];

const BulkCloseModal = ({ ticketIds, onDismiss, onSuccess }: BulkCloseModalProps) => {
  const [resolution, setResolution] = useState("");
  const [notes, setNotes] = useState("");

  const handleSubmit = useCallback(() => {
    if (!resolution) return;
    onSuccess();
  }, [resolution, onSuccess]);

  return (
    <Dialog
      open
      onDismiss={onDismiss}
      title={`Close ${ticketIds.length} ticket${ticketIds.length > 1 ? "s" : ""}`}
      subtitle="Select a resolution for all selected tickets."
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
        },
        {
          text: "Close tickets",
          variant: variants.danger,
          onClick: handleSubmit,
          disabled: !resolution,
        },
      ]}
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-2">
          {RESOLUTIONS.map((r) => (
            <label
              key={r.value}
              className={`flex cursor-pointer items-start gap-3 rounded-xl border p-3 transition-colors ${
                resolution === r.value
                  ? "border-core-primary-fill bg-core-primary-5"
                  : "border-border-5 hover:border-border-20"
              }`}
            >
              <div className="mt-0.5">
                <Radio
                  selected={resolution === r.value}
                  onChange={() => setResolution(r.value)}
                  name="bulk-resolution"
                  value={r.value}
                />
              </div>
              <div className="flex flex-col">
                <span className="text-emphasis-300 font-medium">{r.label}</span>
                <span className="text-200 text-text-primary-70">{r.description}</span>
              </div>
            </label>
          ))}
        </div>
        <Textarea
          id="bulk-close-notes"
          label="Notes (optional)"
          onChange={(value) => setNotes(value)}
          rows={2}
        />
      </div>
    </Dialog>
  );
};

export default BulkCloseModal;
