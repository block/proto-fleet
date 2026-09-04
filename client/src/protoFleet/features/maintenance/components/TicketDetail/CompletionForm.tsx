import { useEffect, useState } from "react";
import { RepairLocation, TicketResolution } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { useInventoryApi } from "@/protoFleet/api/inventory";
import type { PartSelection } from "@/protoFleet/api/maintenance";
import type { PartUsageItem } from "@/protoFleet/features/maintenance/types";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface CompletionFormProps {
  isMinerTicket?: boolean;
  siteId: string | null;
  initialParts?: PartUsageItem[];
  onSubmit: (value: {
    resolution: TicketResolution;
    repairLocation: RepairLocation;
    notes: string;
    partsSelection: PartSelection[];
  }) => Promise<boolean>;
  onCancel: () => void;
}
const RESOLUTION_OPTIONS = [
  { value: String(TicketResolution.REPAIRED), label: "Repaired" },
  { value: String(TicketResolution.REPLACED), label: "Replaced" },
  { value: String(TicketResolution.DEFERRED), label: "Deferred" },
  { value: String(TicketResolution.UNREPAIRABLE), label: "Unrepairable" },
  { value: String(TicketResolution.NO_ACTION_NEEDED), label: "No action needed" },
];
const LOCATION_OPTIONS = [
  { value: String(RepairLocation.ON_RACK), label: "On-rack" },
  { value: String(RepairLocation.REPAIR_BENCH), label: "Repair bench" },
];
const EMPTY_INITIAL_PARTS: PartUsageItem[] = [];
const CompletionForm = ({
  isMinerTicket = true,
  siteId,
  initialParts = EMPTY_INITIAL_PARTS,
  onSubmit,
  onCancel,
}: CompletionFormProps) => {
  const { listPartsBySite } = useInventoryApi();
  const [resolution, setResolution] = useState(TicketResolution.REPAIRED);
  const [repairLocation, setRepairLocation] = useState(RepairLocation.ON_RACK);
  const [parts, setParts] = useState<Array<{ id: bigint; name: string; available: number }>>([]);
  const [quantities, setQuantities] = useState<Record<string, number>>(() =>
    Object.fromEntries(initialParts.map((part) => [part.inventoryPartId, part.quantity])),
  );
  const [notes, setNotes] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!siteId) return;
    const controller = new AbortController();
    queueMicrotask(
      () =>
        void listPartsBySite({
          siteId: BigInt(siteId),
          signal: controller.signal,
          onSuccess: (items) =>
            setParts(
              items.map((item) => ({
                id: item.id,
                name: item.name,
                available:
                  item.onHand -
                  item.allocated +
                  (initialParts.find((part) => part.inventoryPartId === item.id.toString())?.quantity ?? 0),
              })),
            ),
          onError: setError,
        }),
    );
    return () => controller.abort();
  }, [initialParts, listPartsBySite, siteId]);
  const submit = async () => {
    setBusy(true);
    setError(null);
    const selection = parts.flatMap((part) =>
      (quantities[part.id.toString()] ?? 0) > 0
        ? [{ inventoryPartId: part.id, partName: part.name, quantity: quantities[part.id.toString()] }]
        : [],
    );
    const ok = await onSubmit({
      resolution,
      repairLocation: isMinerTicket ? repairLocation : RepairLocation.UNSPECIFIED,
      notes,
      partsSelection: selection,
    });
    setBusy(false);
    if (!ok) setError("Unable to complete repair");
  };
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="resolution"
          label="Mark as"
          options={RESOLUTION_OPTIONS}
          value={String(resolution)}
          onChange={(v) => setResolution(Number(v) as TicketResolution)}
          forceBelow
        />
        {isMinerTicket && (resolution === TicketResolution.REPAIRED || resolution === TicketResolution.REPLACED) ? (
          <Select
            id="repair-location"
            label="Repair location"
            options={LOCATION_OPTIONS}
            value={String(repairLocation)}
            onChange={(v) => setRepairLocation(Number(v) as RepairLocation)}
            forceBelow
          />
        ) : (
          <div />
        )}
      </div>
      {parts.length ? (
        <fieldset className="flex flex-col gap-2">
          <legend className="text-300">Parts used</legend>
          {parts.map((part) => (
            <label key={part.id.toString()} className="flex items-center justify-between gap-3">
              <span>
                {part.name} ({part.available} available)
              </span>
              <input
                aria-label={`${part.name} quantity`}
                type="number"
                min={0}
                max={part.available}
                value={quantities[part.id.toString()] ?? 0}
                onChange={(e) =>
                  setQuantities((old) => ({
                    ...old,
                    [part.id.toString()]: Math.min(
                      part.available,
                      Math.max(0, Number.parseInt(e.target.value || "0", 10)),
                    ),
                  }))
                }
                className="w-20 rounded border p-2"
              />
            </label>
          ))}
        </fieldset>
      ) : null}
      <Textarea id="completion-notes" label="Notes (optional)" onChange={setNotes} rows={3} />
      {error ? <div role="alert">{error}</div> : null}
      <div className="flex justify-end gap-3">
        <Button text="Cancel" variant={variants.secondary} size={buttonSizes.compact} onClick={onCancel} />
        <Button
          text="Complete repair"
          variant={variants.primary}
          size={buttonSizes.compact}
          onClick={() => void submit()}
          loading={busy}
        />
      </div>
    </div>
  );
};
export default CompletionForm;
