import { useCallback, useState } from "react";
import { RepairLocation, TicketResolution } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import type { BulkTicketMutation } from "@/protoFleet/api/maintenance";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface BulkCloseModalProps {
  ticketIds: string[];
  includesMiner?: boolean;
  onDismiss: () => void;
  onSubmit?: (mutation: BulkTicketMutation) => Promise<boolean>;
  onSuccess: () => void;
}
const RESOLUTIONS = [
  { value: TicketResolution.REPAIRED, label: "Repaired", description: "Issue was fixed" },
  { value: TicketResolution.REPLACED, label: "Replaced", description: "Component was swapped out" },
  { value: TicketResolution.NO_ACTION_NEEDED, label: "No action needed", description: "No repair was required" },
  { value: TicketResolution.DEFERRED, label: "Deferred", description: "Moved to a future maintenance window" },
  { value: TicketResolution.UNREPAIRABLE, label: "Unrepairable", description: "Cannot be fixed" },
];
const locationOptions = [
  { value: String(RepairLocation.ON_RACK), label: "On rack" },
  { value: String(RepairLocation.REPAIR_BENCH), label: "Repair bench" },
];
const BulkCloseModal = ({ ticketIds, includesMiner = false, onDismiss, onSubmit, onSuccess }: BulkCloseModalProps) => {
  const [resolution, setResolution] = useState<TicketResolution>();
  const [repairLocation, setRepairLocation] = useState<RepairLocation>();
  const [notes, setNotes] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const requiresLocation =
    includesMiner && (resolution === TicketResolution.REPAIRED || resolution === TicketResolution.REPLACED);
  const handleSubmit = useCallback(async () => {
    if (resolution === undefined || (requiresLocation && repairLocation === undefined)) return;
    setSubmitting(true);
    setError(null);
    const ok =
      (await onSubmit?.({
        case: "bulkClose",
        value: {
          resolution,
          repairLocation: requiresLocation
            ? (repairLocation ?? RepairLocation.UNSPECIFIED)
            : RepairLocation.UNSPECIFIED,
          notes,
        },
      })) ?? true;
    setSubmitting(false);
    if (ok) onSuccess();
    else setError("Unable to close tickets");
  }, [notes, onSubmit, onSuccess, repairLocation, requiresLocation, resolution]);
  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={`Close ${ticketIds.length} ticket${ticketIds.length > 1 ? "s" : ""}`}
      description="Select a resolution for all selected tickets."
      size="standard"
      buttons={[
        { text: "Cancel", variant: variants.secondary, onClick: onDismiss, dismissModalOnClick: false },
        {
          text: "Close tickets",
          variant: variants.primary,
          onClick: () => void handleSubmit(),
          disabled: resolution === undefined || (requiresLocation && repairLocation === undefined),
          loading: submitting,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <table aria-label="Close ticket resolution options" className="w-full table-fixed border-collapse">
          <thead className="sr-only">
            <tr>
              <th>Resolution</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody className="border-y border-border-5">
            {RESOLUTIONS.map((item) => (
              <tr
                key={item.value}
                className="cursor-pointer border-b border-border-5"
                onClick={() => setResolution(item.value)}
              >
                <td className="w-2/5 py-3 pr-3">
                  <label className="flex items-center gap-3">
                    <Radio
                      selected={resolution === item.value}
                      onChange={() => setResolution(item.value)}
                      name="bulk-resolution"
                      value={String(item.value)}
                    />
                    <span>{item.label}</span>
                  </label>
                </td>
                <td className="w-3/5 py-3 pl-3 text-right text-300 text-text-primary-70">{item.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {requiresLocation ? (
          <Select
            id="bulk-repair-location"
            label="Repair location"
            options={locationOptions}
            value={repairLocation === undefined ? "" : String(repairLocation)}
            onChange={(value) => setRepairLocation(Number(value) as RepairLocation)}
          />
        ) : null}
        {resolution !== undefined ? (
          <Textarea id="bulk-close-notes" label="Notes (optional)" onChange={setNotes} rows={2} />
        ) : null}
        {error ? <div role="alert">{error}</div> : null}
      </div>
    </Modal>
  );
};
export default BulkCloseModal;
