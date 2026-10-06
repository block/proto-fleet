import { useCallback, useMemo, useRef, useState } from "react";
import { INFRA_COMPONENTS, MINER_COMPONENTS } from "../../componentOptions";
import MinerTicketPicker from "./MinerTicketPicker";
import { TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useHasPermission } from "@/protoFleet/store";
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
const CATEGORY_OPTIONS = [
  { value: "miner", label: "Miner" },
  { value: "infrastructure", label: "Infrastructure" },
];
type Category = "miner" | "infrastructure";

const createIdempotencyKey = () => {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
};

const CreateTicketModal = ({ onDismiss, onSuccess, prefill }: CreateTicketModalProps) => {
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const canReadMiners = useHasPermission("miner:read");
  const { createTicket } = useMaintenanceApi();
  const options = useMaintenanceOptions();
  const idempotencyKey = useRef(createIdempotencyKey());
  const [category, setCategory] = useState<Category>("miner");
  const [component, setComponent] = useState(prefill?.component ?? "");
  const [minerIdentifier, setMinerIdentifier] = useState(prefill?.minerIdentifier ?? "");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [diagnosis, setDiagnosis] = useState(prefill?.diagnosis ?? "");
  const [siteId, setSiteId] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [urgent, setUrgent] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const components = category === "miner" ? MINER_COMPONENTS : INFRA_COMPONENTS;
  const siteOptions = useMemo(
    () => options.manageableSites.map((site) => ({ value: site.id, label: site.name })),
    [options.manageableSites],
  );
  const defaultSiteId = useMemo(() => {
    const preferredSiteId = prefill?.siteId ?? (activeSite.kind === "site" ? activeSite.id : undefined);
    return (
      options.manageableSites.find((site) => site.id === preferredSiteId)?.id ?? options.manageableSites[0]?.id ?? ""
    );
  }, [activeSite, options.manageableSites, prefill?.siteId]);
  const resolvedSiteId = options.manageableSites.some((site) => site.id === siteId) ? siteId : defaultSiteId;
  const canSubmit = Boolean(
    component && diagnosis.trim() && (category === "miner" ? minerIdentifier : resolvedSiteId) && !submitting,
  );
  const assigneeOptions = useMemo(
    () => [
      { value: "", label: "Unassigned" },
      ...options.assignees.map((item) => ({ value: item.id, label: item.username })),
    ],
    [options.assignees],
  );
  const dismiss = useCallback(() => {
    if (!submitting) onDismiss();
  }, [onDismiss, submitting]);
  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    await createTicket({
      category: category === "miner" ? TicketCategory.MINER : TicketCategory.INFRASTRUCTURE,
      component,
      idempotencyKey: idempotencyKey.current,
      diagnosis,
      urgent,
      minerIdentifier: category === "miner" ? minerIdentifier : undefined,
      alertId: prefill?.alertId,
      assigneeUserId: assigneeId ? BigInt(assigneeId) : undefined,
      siteId: category === "infrastructure" && resolvedSiteId ? BigInt(resolvedSiteId) : undefined,
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
    resolvedSiteId,
    urgent,
  ]);
  return (
    <>
      <Modal
        open
        onDismiss={dismiss}
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
              <Input id="miner-id" label="Miner ID" initValue={minerIdentifier} onChange={setMinerIdentifier} />
              {canReadMiners ? (
                <button type="button" className="mt-2 text-300 underline" onClick={() => setPickerOpen(true)}>
                  Select miner
                </button>
              ) : null}
            </div>
          ) : (
            <Select
              id="site"
              label="Site"
              options={siteOptions}
              value={resolvedSiteId}
              onChange={setSiteId}
              forceBelow
            />
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
      {pickerOpen && canReadMiners ? (
        <MinerTicketPicker
          selected={minerIdentifier}
          manageableSiteIds={options.manageableSites.map((site) => site.id)}
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
