import { useCallback, useMemo, useState } from "react";
import MinerTicketPicker from "./MinerTicketPicker";
import { TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface CreateTicketModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
  prefill?: { alertId?: string; minerIdentifier?: string; component?: string; diagnosis?: string; siteId?: string };
}
const MINER_COMPONENTS = ["Fan", "Hashboard", "PSU", "Control Board"].map((value) => ({ value, label: value }));
const INFRA_COMPONENTS = ["Network", "Electrical", "HVAC", "Cleaning", "Building"].map((value) => ({
  value,
  label: value,
}));
const CATEGORY_OPTIONS = [
  { value: "miner", label: "Miner" },
  { value: "infrastructure", label: "Infrastructure" },
];
type Category = "miner" | "infrastructure";

const CreateTicketModal = ({ onDismiss, onSuccess, prefill }: CreateTicketModalProps) => {
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const { createTicket } = useMaintenanceApi();
  const options = useMaintenanceOptions();
  const defaultSite =
    prefill?.siteId ?? (activeSite.kind === "site" ? activeSite.id : undefined) ?? options.sites[0]?.id ?? "";
  const [category, setCategory] = useState<Category>("miner");
  const [component, setComponent] = useState(prefill?.component ?? "");
  const [minerIdentifier, setMinerIdentifier] = useState(prefill?.minerIdentifier ?? "");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [diagnosis, setDiagnosis] = useState(prefill?.diagnosis ?? "");
  const [siteId, setSiteId] = useState(defaultSite);
  const [assigneeId, setAssigneeId] = useState("");
  const [urgent, setUrgent] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const components = category === "miner" ? MINER_COMPONENTS : INFRA_COMPONENTS;
  const canSubmit = Boolean(
    component && diagnosis.trim() && (category === "miner" ? minerIdentifier : siteId) && !submitting,
  );
  const siteOptions = useMemo(
    () => options.sites.map((site) => ({ value: site.id, label: site.name })),
    [options.sites],
  );
  const assigneeOptions = useMemo(
    () => [
      { value: "", label: "Unassigned" },
      ...options.assignees.map((item) => ({ value: item.id, label: item.username })),
    ],
    [options.assignees],
  );
  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    await createTicket({
      category: category === "miner" ? TicketCategory.MINER : TicketCategory.INFRASTRUCTURE,
      component,
      diagnosis,
      urgent,
      minerIdentifier: category === "miner" ? minerIdentifier : undefined,
      alertId: prefill?.alertId,
      assigneeUserId: assigneeId ? BigInt(assigneeId) : undefined,
      siteId: category === "infrastructure" && siteId ? BigInt(siteId) : undefined,
      onSuccess: () => onSuccess(),
      onError: setError,
      onFinally: () => setSubmitting(false),
    });
  }, [
    assigneeId,
    canSubmit,
    category,
    component,
    createTicket,
    diagnosis,
    minerIdentifier,
    onSuccess,
    prefill?.alertId,
    siteId,
    urgent,
  ]);
  return (
    <>
      <Modal
        open
        onDismiss={onDismiss}
        title="New ticket"
        buttons={[
          {
            text: "Create ticket",
            variant: variants.primary,
            onClick: () => void submit(),
            disabled: !canSubmit,
            loading: submitting,
            dismissModalOnClick: false,
          },
        ]}
      >
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="category"
              label="Category"
              options={CATEGORY_OPTIONS}
              value={category}
              onChange={(value) => {
                setCategory(value as Category);
                setComponent("");
              }}
              forceBelow
            />
            <Select
              id="component"
              label="Component"
              options={components}
              value={component}
              onChange={setComponent}
              forceBelow
            />
          </div>
          {category === "miner" ? (
            <div>
              <Input id="miner-id" label="Miner ID" initValue={minerIdentifier} disabled />
              <button type="button" className="mt-2 text-300 underline" onClick={() => setPickerOpen(true)}>
                Select miner
              </button>
            </div>
          ) : (
            <Select id="site" label="Site" options={siteOptions} value={siteId} onChange={setSiteId} forceBelow />
          )}
          <Textarea id="diagnosis" label="Issue description" onChange={setDiagnosis} rows={3} />
          <Select
            id="assignee"
            label="Assignee"
            options={assigneeOptions}
            value={assigneeId}
            onChange={setAssigneeId}
            forceBelow
          />
          <label className="flex items-center gap-2 text-300">
            <Checkbox checked={urgent} onChange={(event) => setUrgent(event.target.checked)} />
            Mark as urgent
          </label>
          {error ? <div role="alert">{error}</div> : null}
        </div>
      </Modal>
      {pickerOpen ? (
        <MinerTicketPicker
          selected={minerIdentifier}
          onDismiss={() => setPickerOpen(false)}
          onSelect={(identifier) => {
            setMinerIdentifier(identifier);
            setPickerOpen(false);
          }}
        />
      ) : null}
    </>
  );
};
export default CreateTicketModal;
