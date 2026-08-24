import { useCallback, useState } from "react";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
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
  const [, setNotes] = useState("");

  const handleSubmit = useCallback(() => {
    if (!resolution) return;
    onSuccess();
  }, [resolution, onSuccess]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={`Close ${ticketIds.length} ticket${ticketIds.length > 1 ? "s" : ""}`}
      description="Select a resolution for all selected tickets."
      size="standard"
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
          dismissModalOnClick: false,
        },
        {
          text: "Close tickets",
          variant: variants.primary,
          onClick: handleSubmit,
          disabled: !resolution,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <table aria-label="Close ticket resolution options" className="w-full table-fixed border-collapse">
          <thead className="sr-only">
            <tr>
              <th scope="col">Resolution</th>
              <th scope="col">Description</th>
            </tr>
          </thead>
          <tbody className="border-y border-border-5">
            {RESOLUTIONS.map((r) => (
              <tr
                key={r.value}
                className="cursor-pointer border-b border-border-5 transition-colors last:border-b-0 hover:bg-surface-5"
                onClick={() => setResolution(r.value)}
              >
                <td className="w-2/5 py-3 pr-3">
                  <label className="flex cursor-pointer items-center gap-3">
                    <Radio
                      selected={resolution === r.value}
                      onChange={() => setResolution(r.value)}
                      name="bulk-resolution"
                      value={r.value}
                    />
                    <span className="text-emphasis-300 font-medium">{r.label}</span>
                  </label>
                </td>
                <td className="w-3/5 py-3 pl-3 text-right text-300 text-text-primary-70">{r.description}</td>
              </tr>
            ))}
          </tbody>
        </table>

        {resolution ? (
          <Textarea id="bulk-close-notes" label="Notes (optional)" onChange={(value) => setNotes(value)} rows={2} />
        ) : null}
      </div>
    </Modal>
  );
};

export default BulkCloseModal;
