import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
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
            <button
              key={r.value}
              type="button"
              className={`flex cursor-pointer items-start gap-3 rounded-xl border p-3 text-left transition-colors ${
                resolution === r.value
                  ? "border-core-primary-fill bg-core-primary-5"
                  : "border-border-5 hover:border-border-20"
              }`}
              onClick={() => setResolution(r.value)}
            >
              <div
                className={`mt-0.5 flex h-4.5 w-4.5 flex-shrink-0 items-center justify-center rounded-full border-2 ${
                  resolution === r.value ? "border-core-primary-fill" : "border-border-20"
                }`}
              >
                {resolution === r.value && (
                  <div className="h-2 w-2 rounded-full bg-core-primary-fill" />
                )}
              </div>
              <div className="flex flex-col">
                <span className="text-emphasis-300 font-medium">{r.label}</span>
                <span className="text-200 text-text-primary-70">{r.description}</span>
              </div>
            </button>
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
