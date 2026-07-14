import { type ReactNode, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
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

const DURATION_UNIT_OPTIONS = [
  { value: "seconds", label: "seconds" },
  { value: "minutes", label: "minutes" },
  { value: "hours", label: "hours" },
];

const UNIT_TO_SECONDS: Record<string, number> = { seconds: 1, minutes: 60, hours: 3600 };

// Grafana caps alert-rule titles at 190 characters (mirrored server-side).
const MAX_NAME_LENGTH = 190;

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

const triggerSummary = (template: UserRuleTemplate, amount: string, unit: string, durationSeconds: number): string => {
  const dur = describeDuration(durationSeconds);
  switch (template) {
    case "offline":
      return `Alerts when a miner is offline for more than ${dur}.`;
    case "hashrate":
      return unit === "%"
        ? `Alerts when a miner hashes below ${amount || "…"}% of its expected rate for more than ${dur}.`
        : `Alerts when a miner hashes below ${amount || "…"} ${unit} for more than ${dur}.`;
    case "temperature":
      return `Alerts when a miner's max sensor temperature stays above ${amount || "…"}${unit} for more than ${dur}.`;
  }
};

const Row = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="grid items-center gap-4 laptop:grid-cols-[minmax(9rem,0.9fr)_minmax(0,2fr)]">
    <div className="text-300 text-text-primary">{label}</div>
    {children}
  </div>
);

interface AddRuleModalProps {
  open: boolean;
  editingRule: Rule | null;
  onDismiss: () => void;
}

const AddRuleModal = ({ open, editingRule, onDismiss }: AddRuleModalProps) => {
  const { createRule, updateRule } = useAlertsContext();
  const preferredTemperatureUnit: TemperatureFieldUnit = useTemperatureUnit() === "F" ? "°F" : "°C";

  const isEditing = editingRule != null;

  const [template, setTemplate] = useState<UserRuleTemplate>("offline");
  const [name, setName] = useState("");
  const [amount, setAmount] = useState("");
  const [hashrateUnit, setHashrateUnit] = useState<HashrateFieldUnit>("%");
  const [temperatureUnit, setTemperatureUnit] = useState<TemperatureFieldUnit>("°C");
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

  const [syncedFor, setSyncedFor] = useState<string | null>(null);
  const syncKey = open ? (editingRule?.id ?? "__add__") : null;
  if (syncedFor !== syncKey) {
    setSyncedFor(syncKey);
    if (open) {
      const cfg = editingRule?.config;
      setTemperatureUnit(preferredTemperatureUnit);
      if (cfg?.hashrate) {
        setTemplate("hashrate");
        setAmount(String(cfg.hashrate.value));
        setHashrateUnit(
          cfg.hashrate.mode === "absolute" ? (`${cfg.hashrate.unit ?? "TH"}/s` as HashrateFieldUnit) : "%",
        );
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
      } else {
        setTemplate("offline");
        setAmount(DEFAULT_AMOUNT.offline);
      }
      setName(cfg?.name ?? editingRule?.name ?? "");
      setDurationSeconds(cfg?.duration_seconds ?? editingRule?.duration_seconds ?? DEFAULT_DURATION.offline);
      setErrorMsg("");
      setSaving(false);
    }
  }

  const clearError = () => setErrorMsg("");

  const handleTemplateChange = (next: UserRuleTemplate) => {
    setTemplate(next);
    setAmount(DEFAULT_AMOUNT[next]);
    setHashrateUnit("%");
    setTemperatureUnit(preferredTemperatureUnit);
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
      if (hashrateUnit === "%") {
        if (value > 100) return fail("Percent of expected must be at most 100");
        return { ...base, hashrate: { mode: "pct_expected" as const, value } };
      }
      return {
        ...base,
        hashrate: {
          mode: "absolute" as const,
          value,
          unit: hashrateUnit === "PH/s" ? ("PH" as const) : ("TH" as const),
        },
      };
    }
    if (!Number.isFinite(value)) return fail("Enter a temperature threshold");
    const celsius = temperatureUnit === "°F" ? convertFtoC(value) : value;
    if (celsius <= 0 || celsius > 150) {
      return fail(
        temperatureUnit === "°F"
          ? "Temperature must be greater than 32 and at most 302 °F"
          : "Temperature must be greater than 0 and at most 150 °C",
      );
    }
    return { ...base, temperature: { max_celsius: round2(celsius) } };
  };

  const handleSave = async () => {
    const config = buildConfig();
    if (!config) return;
    setSaving(true);
    try {
      if (isEditing && editingRule) {
        await updateRule(editingRule.id, config);
        pushToast({ message: `Rule updated: ${config.name}`, status: STATUSES.success });
      } else {
        await createRule(config);
        pushToast({ message: `Rule created: ${config.name}`, status: STATUSES.success });
      }
      onDismiss();
    } catch (error) {
      pushToast({ message: getErrorMessage(error, "Failed to save rule"), status: STATUSES.error });
      setSaving(false);
    }
  };

  const summary = triggerSummary(
    template,
    amount,
    template === "temperature" ? temperatureUnit : hashrateUnit,
    durationSeconds,
  );

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

        {template === "hashrate" ? (
          <Row label="drops below">
            <div className="grid grid-cols-2 gap-4">
              <Input
                id="rule-hashrate-amount"
                label="Amount"
                initValue={amount}
                inputMode="decimal"
                onChange={(value) => {
                  setAmount(value);
                  clearError();
                }}
              />
              <Select
                id="rule-hashrate-unit"
                label="Unit"
                options={HASHRATE_UNIT_OPTIONS}
                value={hashrateUnit}
                forceBelow
                onChange={(value) => {
                  setHashrateUnit(value as HashrateFieldUnit);
                  clearError();
                }}
              />
            </div>
          </Row>
        ) : null}

        {template === "temperature" ? (
          <Row label="rises above">
            <div className="grid grid-cols-2 gap-4">
              <Input
                id="rule-temperature-amount"
                label="Amount"
                initValue={amount}
                inputMode="decimal"
                onChange={(value) => {
                  setAmount(value);
                  clearError();
                }}
              />
              <Select
                id="rule-temperature-unit"
                label="Unit"
                options={TEMPERATURE_UNIT_OPTIONS}
                value={temperatureUnit}
                forceBelow
                onChange={(value) => {
                  setTemperatureUnit(value as TemperatureFieldUnit);
                  clearError();
                }}
              />
            </div>
          </Row>
        ) : null}

        <Row label="for longer than">
          <div className="grid grid-cols-2 gap-4">
            <Input
              id="rule-duration-amount"
              label="Duration"
              initValue={durationAmount}
              inputMode="decimal"
              onChange={(value) => {
                setDurationAmount(value);
                clearError();
              }}
            />
            <Select
              id="rule-duration-unit"
              label="Duration unit"
              options={DURATION_UNIT_OPTIONS}
              value={durationUnit}
              forceBelow
              onChange={(value) => {
                setDurationUnit(value);
                clearError();
              }}
            />
          </div>
        </Row>

        <p className="text-300 text-text-primary-50">{summary}</p>
      </div>
    </Modal>
  );
};

export default AddRuleModal;
