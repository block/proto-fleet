import { useCallback, useMemo, useRef, useState } from "react";

import { mockTickets, REPAIR_TECHNICIANS } from "../../mockData";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface CreateTicketModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
  prefill?: {
    alertId?: string;
    minerIdentifier?: string;
    component?: string;
    diagnosis?: string;
    siteId?: string;
  };
}

const MINER_COMPONENTS = [
  { value: "Fan", label: "Fan" },
  { value: "Hashboard", label: "Hashboard" },
  { value: "PSU", label: "PSU" },
  { value: "Control Board", label: "Control Board" },
];

const INFRA_COMPONENTS = [
  { value: "Network", label: "Network" },
  { value: "Electrical", label: "Electrical" },
  { value: "HVAC", label: "HVAC" },
  { value: "Cleaning", label: "Cleaning" },
  { value: "Building", label: "Building" },
];

const SITE_OPTIONS = [
  { value: "Denver", label: "Denver" },
  { value: "Austin", label: "Austin" },
  { value: "Miami", label: "Miami" },
  { value: "Marfa", label: "Marfa" },
];

const ASSIGNEE_OPTIONS = REPAIR_TECHNICIANS.map((t) => ({ value: t, label: t }));
const CATEGORY_OPTIONS = [
  { value: "miner", label: "Miner" },
  { value: "infrastructure", label: "Infrastructure" },
];

type TicketCategory = "miner" | "infrastructure";

const siteValueFromSlug = (slug: string) =>
  SITE_OPTIONS.find(({ value }) => value.toLowerCase().replaceAll(" ", "-") === slug)?.value;

const KNOWN_MINERS = (() => {
  const set = new Set<string>();
  mockTickets.forEach((t) => {
    if (t.minerIdentifier) set.add(t.minerIdentifier);
  });
  for (let i = 1; i <= 50; i++) set.add(`M${String(i).padStart(4, "0")}`);
  return [...set].sort();
})();

const CreateTicketModal = ({ onDismiss, onSuccess, prefill }: CreateTicketModalProps) => {
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const defaultSite =
    prefill?.siteId ??
    (activeSite.kind === "site" ? siteValueFromSlug(activeSite.slug) : undefined) ??
    SITE_OPTIONS[0].value;
  const [category, setCategory] = useState<TicketCategory>("miner");
  const [component, setComponent] = useState(prefill?.component ?? "");
  const [minerIdentifier, setMinerIdentifier] = useState(prefill?.minerIdentifier ?? "");
  const [minerQuery, setMinerQuery] = useState(prefill?.minerIdentifier ?? "");
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [diagnosis, setDiagnosis] = useState(prefill?.diagnosis ?? "");
  const [site, setSite] = useState(defaultSite);
  const [assignee, setAssignee] = useState("");
  const [urgent, setUrgent] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const suggestionsRef = useRef<HTMLDivElement>(null);

  const componentOptions = category === "miner" ? MINER_COMPONENTS : INFRA_COMPONENTS;
  const canSubmit = Boolean(
    component && diagnosis && site && (category !== "miner" || minerIdentifier) && !isSubmitting,
  );

  const filteredMiners = useMemo(() => {
    if (!minerQuery) return KNOWN_MINERS.slice(0, 8);
    const q = minerQuery.toLowerCase();
    return KNOWN_MINERS.filter((m) => m.toLowerCase().includes(q)).slice(0, 8);
  }, [minerQuery]);

  const handleMinerInput = useCallback((value: string) => {
    setMinerQuery(value);
    setMinerIdentifier(value);
    setShowSuggestions(true);
  }, []);

  const selectMiner = useCallback((id: string) => {
    setMinerIdentifier(id);
    setMinerQuery(id);
    setShowSuggestions(false);
  }, []);

  const handleSubmit = useCallback(() => {
    if (!canSubmit) return;
    setIsSubmitting(true);
    onSuccess();
  }, [canSubmit, onSuccess]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="New ticket"
      buttons={[
        {
          text: "Create ticket",
          variant: variants.primary,
          onClick: handleSubmit,
          disabled: !canSubmit,
          loading: isSubmitting,
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
              setCategory(value as TicketCategory);
              setComponent("");
            }}
            forceBelow
          />
          <Select
            id="component"
            label="Component"
            options={componentOptions}
            value={component}
            onChange={setComponent}
            forceBelow
          />
        </div>

        {category === "miner" ? (
          <div className="relative">
            <Input
              id="miner-id"
              label="Miner ID"
              initValue={minerQuery}
              onChange={handleMinerInput}
              onFocus={() => setShowSuggestions(true)}
              onBlur={() => setTimeout(() => setShowSuggestions(false), 200)}
            />
            {showSuggestions && filteredMiners.length > 0 ? (
              <div
                ref={suggestionsRef}
                className="absolute top-full right-0 left-0 z-20 mt-1 max-h-48 overflow-y-auto rounded-xl border border-border-5 bg-surface-elevated-base shadow-300"
              >
                {filteredMiners.map((m) => (
                  <div key={m} className="px-2" onMouseDown={(e) => e.preventDefault()}>
                    <Row compact className="text-emphasis-300" onClick={() => selectMiner(m)}>
                      {m}
                    </Row>
                  </div>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}

        <Textarea id="diagnosis" label="Issue description" onChange={(value) => setDiagnosis(value)} rows={3} />

        <div className="grid grid-cols-2 gap-3">
          <Select id="site" label="Site" options={SITE_OPTIONS} value={site} onChange={setSite} forceBelow />
          <Select
            id="assignee"
            label="Assignee"
            options={ASSIGNEE_OPTIONS}
            value={assignee}
            onChange={setAssignee}
            forceBelow
          />
        </div>

        <label className="flex items-center gap-2 text-300">
          <Checkbox checked={urgent} onChange={(e) => setUrgent(e.target.checked)} />
          Mark as urgent
        </label>
      </div>
    </Modal>
  );
};

export default CreateTicketModal;
