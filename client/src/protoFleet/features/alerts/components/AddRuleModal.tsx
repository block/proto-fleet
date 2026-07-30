import { type ReactNode, useRef, useState } from "react";
import DeliveryPicker from "./DeliveryPicker";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useDeliveryRouting } from "@/protoFleet/features/alerts/api/useDeliveryRouting";
import type { Rule, RuleConfig } from "@/protoFleet/features/alerts/types";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { convertCtoF, convertFtoC } from "@/shared/utils/telemetryFormat";

type UserRuleTemplate = "offline" | "hashrate" | "temperature";

const TEMPLATE_OPTIONS: { value: UserRuleTemplate; label: string }[] = [
  { value: "offline", label: "Offline" },
  { value: "hashrate", label: "Hashrate" },
  { value: "temperature", label: "Temperature" },
];

type HashrateFieldUnit = "%" | "TH/s" | "PH/s";

const HASHRATE_UNIT_OPTIONS: { value: HashrateFieldUnit; label: string }[] = [
  { value: "%", label: "% of expected" },
  { value: "TH/s", label: "TH/s" },
  { value: "PH/s", label: "PH/s" },
];

type TemperatureFieldUnit = "°C" | "°F";

const TEMPERATURE_UNIT_OPTIONS: { value: TemperatureFieldUnit; label: string }[] = [
  { value: "°C", label: "°C" },
  { value: "°F", label: "°F" },
];

// The threshold unit select shows hashrate units or temperature units depending on template.
type ThresholdUnit = HashrateFieldUnit | TemperatureFieldUnit;

const DURATION_UNIT_OPTIONS = [
  { value: "seconds", label: "seconds" },
  { value: "minutes", label: "minutes" },
  { value: "hours", label: "hours" },
];

const UNIT_TO_SECONDS: Record<string, number> = { seconds: 1, minutes: 60, hours: 3600 };

// Grafana caps alert-rule titles at 190 characters (mirrored server-side).
const MAX_NAME_LENGTH = 190;

// Server-side threshold bounds (validateRuleConfig); °F copy derives from these.
const MIN_CELSIUS = 0;
const MAX_CELSIUS = 150;
const MIN_HASHRATE_PERCENT = 0.01;

const DEFAULT_DURATION: Record<UserRuleTemplate, number> = { offline: 1800, hashrate: 1200, temperature: 1200 };
const DEFAULT_AMOUNT: Record<UserRuleTemplate, string> = { offline: "", hashrate: "75", temperature: "70" };

const getDurationUnit = (seconds: number): string => {
  if (seconds > 0 && seconds % UNIT_TO_SECONDS.hours === 0) return "hours";
  if (seconds > 0 && seconds % UNIT_TO_SECONDS.minutes === 0) return "minutes";
  return "seconds";
};

const formatDurationAmount = (seconds: number, unit: string): string => {
  const amount = seconds / UNIT_TO_SECONDS[unit];
  return Number.isInteger(amount) ? String(amount) : amount.toFixed(1);
};

// parseFloat accepts trailing garbage ("50abc" → 50); Number rejects it, and
// the empty-string guard avoids Number("") === 0.
const strictNumber = (raw: string): number => {
  const trimmed = raw.trim();
  if (trimmed === "") return NaN;
  return Number(trimmed);
};

const round2 = (value: number): number => Math.round(value * 100) / 100;

const describeDuration = (seconds: number): string => {
  if (!Number.isFinite(seconds) || seconds <= 0) return "…";
  const unit = getDurationUnit(seconds);
  const amount = formatDurationAmount(seconds, unit);
  const singular = unit.slice(0, -1);
  return amount === "1" ? `1 ${singular}` : `${amount} ${unit}`;
};

// Phrased like the server's compiled rule summaries (compileTemplate), so the
// preview reads the same as the saved rule's Condition column.
const triggerSummary = (template: UserRuleTemplate, amount: string, unit: string, durationSeconds: number): string => {
  const dur = describeDuration(durationSeconds);
  switch (template) {
    case "offline":
      return `Device is offline for at least ${dur}.`;
    case "hashrate":
      return unit === "%"
        ? `Device hashrate is below ${amount || "…"}% of expected for at least ${dur}.`
        : `Device hashrate is below ${amount || "…"} ${unit} for at least ${dur}.`;
    case "temperature":
      return `Max sensor temperature for device is above ${amount || "…"}${unit} for at least ${dur}.`;
  }
};

const Row = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="grid items-center gap-4 laptop:grid-cols-[minmax(9rem,0.9fr)_minmax(0,2fr)]">
    <div className="text-300 text-text-primary">{label}</div>
    {children}
  </div>
);

interface AmountUnitRowProps {
  rowLabel: string;
  idPrefix: string;
  amountLabel: string;
  unitLabel: string;
  amount: string;
  onAmountChange: (value: string) => void;
  unitOptions: { value: string; label: string }[];
  unit: string;
  onUnitChange: (value: string) => void;
}

const AmountUnitRow = ({
  rowLabel,
  idPrefix,
  amountLabel,
  unitLabel,
  amount,
  onAmountChange,
  unitOptions,
  unit,
  onUnitChange,
}: AmountUnitRowProps) => (
  <Row label={rowLabel}>
    <div className="grid grid-cols-2 gap-4">
      <Input
        id={`${idPrefix}-amount`}
        label={amountLabel}
        initValue={amount}
        inputMode="decimal"
        onChange={onAmountChange}
      />
      <Select
        id={`${idPrefix}-unit`}
        label={unitLabel}
        options={unitOptions}
        value={unit}
        forceBelow
        onChange={onUnitChange}
      />
    </div>
  </Row>
);

interface AddRuleModalProps {
  open: boolean;
  editingRule: Rule | null;
  onDismiss: () => void;
}

const AddRuleModal = ({ open, editingRule, onDismiss }: AddRuleModalProps) => {
  const { createRule, updateRule } = useAlertsContext();
  // Delivery routing, applied with creation; edits keep the dedicated Edit delivery action.
  const routing = useDeliveryRouting();
  const preferredTemperatureUnit: TemperatureFieldUnit = useTemperatureUnit() === "F" ? "°F" : "°C";

  const isEditing = editingRule != null;

  const [template, setTemplate] = useState<UserRuleTemplate>("offline");
  const [name, setName] = useState("");
  const [amount, setAmount] = useState("");
  const [unit, setUnit] = useState<ThresholdUnit>("%");
  // Duration is raw text + unit; deriving them from parsed seconds would
  // rewrite the field (and flip the unit) under the user's cursor.
  const [durationAmount, setDurationAmount] = useState("30");
  const [durationUnit, setDurationUnit] = useState("minutes");
  const [errorMsg, setErrorMsg] = useState("");
  const [saving, setSaving] = useState(false);

  const setDurationSeconds = (seconds: number) => {
    const unit = getDurationUnit(seconds);
    setDurationUnit(unit);
    setDurationAmount(formatDurationAmount(seconds, unit));
  };

  // The modal stays mounted across sessions; a save resolving after a
  // dismiss-and-reopen must not dismiss or toast into the newer session.
  const saveSessionRef = useRef(0);

  const [syncedFor, setSyncedFor] = useState<string | null>(null);
  const syncKey = open ? (editingRule?.id ?? "__add__") : null;
  if (syncedFor !== syncKey) {
    setSyncedFor(syncKey);
    if (open) {
      saveSessionRef.current += 1;
      const cfg = editingRule?.config;
      if (cfg?.hashrate) {
        setTemplate("hashrate");
        setAmount(String(cfg.hashrate.value));
        setUnit(cfg.hashrate.mode === "absolute" ? (`${cfg.hashrate.unit ?? "TH"}/s` as HashrateFieldUnit) : "%");
      } else if (cfg?.temperature) {
        setTemplate("temperature");
        // Stored value is Celsius; present it in the preferred unit.
        setAmount(
          String(
            preferredTemperatureUnit === "°F"
              ? round2(convertCtoF(cfg.temperature.max_celsius))
              : cfg.temperature.max_celsius,
          ),
        );
        setUnit(preferredTemperatureUnit);
      } else {
        setTemplate("offline");
        setAmount(DEFAULT_AMOUNT.offline);
        setUnit("%");
      }
      setName(cfg?.name ?? editingRule?.name ?? "");
      setDurationSeconds(cfg?.duration_seconds ?? editingRule?.duration_seconds ?? DEFAULT_DURATION.offline);
      routing.reset(null);
      setErrorMsg("");
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  const handleTemplateChange = (next: UserRuleTemplate) => {
    setTemplate(next);
    // DEFAULT_AMOUNT.temperature is Celsius; convert when the field opens in °F.
    setAmount(
      next === "temperature" && preferredTemperatureUnit === "°F"
        ? String(round2(convertCtoF(Number(DEFAULT_AMOUNT.temperature))))
        : DEFAULT_AMOUNT[next],
    );
    setUnit(next === "temperature" ? preferredTemperatureUnit : "%");
    setDurationSeconds(DEFAULT_DURATION[next]);
    clearError();
  };

  const durationSeconds = Math.round(strictNumber(durationAmount) * UNIT_TO_SECONDS[durationUnit]);

  const buildConfig = (): RuleConfig | null => {
    const fail = (message: string) => {
      setErrorMsg(message);
      return null;
    };
    const trimmed = name.trim();
    if (!trimmed) return fail("Give the rule a name");
    if (trimmed.length > MAX_NAME_LENGTH) return fail(`Rule names are limited to ${MAX_NAME_LENGTH} characters`);
    if (!Number.isFinite(durationSeconds)) return fail("Enter a duration");
    if (durationSeconds < 60 || durationSeconds > 86400) {
      return fail("Duration must be between 1 minute and 24 hours");
    }
    const base = { name: trimmed, duration_seconds: durationSeconds };
    if (template === "offline") return { ...base, offline: {} };
    const value = strictNumber(amount);
    if (template === "hashrate") {
      if (!Number.isFinite(value) || value <= 0) return fail("Enter a threshold greater than 0");
      if (unit === "%") {
        if (value < MIN_HASHRATE_PERCENT || value > 100) {
          return fail(`Percent of expected must be between ${MIN_HASHRATE_PERCENT} and 100`);
        }
        return { ...base, hashrate: { mode: "pct_expected" as const, value } };
      }
      return {
        ...base,
        hashrate: {
          mode: "absolute" as const,
          value,
          unit: unit === "PH/s" ? ("PH" as const) : ("TH" as const),
        },
      };
    }
    if (!Number.isFinite(value)) return fail("Enter a temperature threshold");
    const celsius = unit === "°F" ? convertFtoC(value) : value;
    if (celsius <= MIN_CELSIUS || celsius > MAX_CELSIUS) {
      return fail(
        unit === "°F"
          ? `Temperature must be greater than ${convertCtoF(MIN_CELSIUS)} and at most ${convertCtoF(MAX_CELSIUS)} °F`
          : `Temperature must be greater than ${MIN_CELSIUS} and at most ${MAX_CELSIUS} °C`,
      );
    }
    return { ...base, temperature: { max_celsius: round2(celsius) } };
  };

  const handleSave = async () => {
    const config = buildConfig();
    if (!config) return;
    if (!isEditing) {
      const invalid = routing.validate();
      if (invalid) {
        setErrorMsg(invalid);
        return;
      }
    }
    const session = saveSessionRef.current;
    setSaving(true);
    try {
      if (isEditing && editingRule) {
        await updateRule(editingRule.id, config);
      } else {
        // Default routing is the absence of a policy; omit it on the wire like the domain does.
        const ruleRouting = routing.toRuleRouting();
        await createRule(config, ruleRouting.mode === "default" ? undefined : ruleRouting);
      }
      if (saveSessionRef.current !== session) return;
      pushToast({
        message: isEditing ? `Rule updated: ${config.name}` : `Rule created: ${config.name}`,
        status: STATUSES.success,
      });
      onDismiss();
    } catch (error) {
      if (saveSessionRef.current !== session) return;
      pushToast({ message: getErrorMessage(error, "Failed to save rule"), status: STATUSES.error });
      setSaving(false);
    }
  };

  const summary = triggerSummary(template, amount, unit, durationSeconds);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={isEditing ? "Edit rule" : "Add rule"}
      description="Alert on a fleet metric when it crosses your threshold."
      buttons={[
        {
          text: saving ? "Saving…" : isEditing ? "Save changes" : "Save rule",
          onClick: () => {
            void handleSave();
          },
          variant: variants.primary,
          dismissModalOnClick: false,
          disabled: saving,
        },
      ]}
      divider={false}
    >
      {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

      <div className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-4">
          <Input
            id="rule-name"
            label="Name"
            initValue={name}
            onChange={(value) => {
              setName(value);
              clearError();
            }}
          />
          <Select
            id="rule-template"
            label="Metric"
            options={TEMPLATE_OPTIONS}
            value={template}
            forceBelow
            onChange={(value) => handleTemplateChange(value as UserRuleTemplate)}
          />
        </div>

        {template !== "offline" ? (
          <AmountUnitRow
            rowLabel={template === "hashrate" ? "drops below" : "rises above"}
            idPrefix={`rule-${template}`}
            amountLabel="Amount"
            unitLabel="Unit"
            amount={amount}
            onAmountChange={(value) => {
              setAmount(value);
              clearError();
            }}
            unitOptions={template === "hashrate" ? HASHRATE_UNIT_OPTIONS : TEMPERATURE_UNIT_OPTIONS}
            unit={unit}
            onUnitChange={(value) => {
              const next = value as ThresholdUnit;
              // °C↔°F and TH/s↔PH/s are changes of scale, not intent: convert
              // the entered amount so the threshold keeps meaning what the user
              // typed. %↔absolute is a mode change and keeps the number.
              const parsed = strictNumber(amount);
              if (next !== unit && Number.isFinite(parsed)) {
                if (template === "temperature") {
                  setAmount(String(round2(next === "°F" ? convertCtoF(parsed) : convertFtoC(parsed))));
                } else if (unit === "TH/s" && next === "PH/s") {
                  setAmount(String(parsed / 1000));
                } else if (unit === "PH/s" && next === "TH/s") {
                  setAmount(String(parsed * 1000));
                }
              }
              setUnit(next);
              clearError();
            }}
          />
        ) : null}

        <AmountUnitRow
          rowLabel="for longer than"
          idPrefix="rule-duration"
          amountLabel="Duration"
          unitLabel="Duration unit"
          amount={durationAmount}
          onAmountChange={(value) => {
            setDurationAmount(value);
            clearError();
          }}
          unitOptions={DURATION_UNIT_OPTIONS}
          unit={durationUnit}
          onUnitChange={(value) => {
            // Like the threshold units, this is a change of scale: convert the
            // amount so "1 hour" doesn't silently become "1 minute".
            if (value !== durationUnit) {
              const parsed = strictNumber(durationAmount);
              if (Number.isFinite(parsed)) {
                const seconds = parsed * UNIT_TO_SECONDS[durationUnit];
                setDurationAmount(String(Number((seconds / UNIT_TO_SECONDS[value]).toFixed(4))));
              }
            }
            setDurationUnit(value);
            clearError();
          }}
        />

        <p className="text-300 text-text-primary-50">{summary}</p>

        {!isEditing ? (
          <>
            <div className="pt-2 text-300 text-text-primary">Delivery</div>
            <DeliveryPicker
              key={routing.sessionKey}
              mode={routing.mode}
              onModeChange={(next) => {
                routing.setMode(next);
                clearError();
              }}
              channels={routing.channels}
              channelsLoaded={routing.channelsLoaded}
              selectedIds={routing.selectedIds}
              onToggleChannel={(id) => {
                routing.toggleChannel(id);
                clearError();
              }}
            />
          </>
        ) : null}
      </div>
    </Modal>
  );
};

export default AddRuleModal;
