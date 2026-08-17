import { type ReactNode, useRef, useState } from "react";
import DeliveryPicker from "./DeliveryPicker";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useAlertScope, type UseAlertScopeResult } from "@/protoFleet/features/alerts/api/useAlertScope";
import { useDeliveryRouting } from "@/protoFleet/features/alerts/api/useDeliveryRouting";
import { useScopeSampleMiners } from "@/protoFleet/features/alerts/api/useScopeSampleMiners";
import { scopePartLabels } from "@/protoFleet/features/alerts/lib/scopeLabels";
import type { Rule, RuleConfig } from "@/protoFleet/features/alerts/types";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";
import BuildingSelectionModal from "@/protoFleet/features/settings/components/Schedules/BuildingSelectionModal";
import GroupSelectionModal from "@/protoFleet/features/settings/components/Schedules/GroupSelectionModal";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import RackSelectionModal from "@/protoFleet/features/settings/components/Schedules/RackSelectionModal";
import SiteSelectionModal from "@/protoFleet/features/settings/components/Schedules/SiteSelectionModal";
import { useHasPermission, useTemperatureUnit } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";
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

// Server-side scope caps (maxRuleScopePlacementIDs / maxRuleScopeDeviceIDs), checked here for a friendly error.
const MAX_SCOPE_PLACEMENT = 100;
const MAX_SCOPE_MINERS = 500;

const DEFAULT_DURATION: Record<UserRuleTemplate, number> = { offline: 1800, hashrate: 1200, temperature: 1200 };
const DEFAULT_AMOUNT: Record<UserRuleTemplate, string> = { offline: "", hashrate: "75", temperature: "70" };

const CONDITION_SECTION_TITLE: Record<UserRuleTemplate, string> = {
  offline: "Alert me when a miner is offline",
  hashrate: "Alert me when hashrate",
  temperature: "Alert me when temperature",
};

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
      return `This alert triggers when included miners are offline for over ${dur}.`;
    case "hashrate":
      return unit === "%"
        ? `This alert triggers when included miners hashrate is less than ${amount || "…"}% of expected for ${dur}.`
        : `This alert triggers when included miners hashrate is less than ${amount || "…"} ${unit} for ${dur}.`;
    case "temperature":
      return `This alert triggers when included miners exceed a temperature of ${amount || "…"}${unit} for longer than ${dur}.`;
  }
};

// Mirrors the union semantics the server compiles: placements OR explicit miners; empty is org-wide.
const scopeSummary = (scope: UseAlertScopeResult): { title: string; detail: string } => {
  if (scope.isOrgWide) {
    return { title: "Applies to all miners", detail: "Every miner in the organization is included." };
  }
  // Same dimension enumeration as the rules table's Applies-to column.
  const placements = scopePartLabels({
    allSites: scope.allSites,
    sites: scope.siteIds.length,
    buildings: scope.buildingIds.length,
    racks: scope.rackIds.length,
    groups: scope.groupIds.length,
    miners: 0,
  }).map((part) => part.toLowerCase());
  const miners =
    scope.deviceIds.length > 0
      ? `${getTargetButtonLabel(scope.deviceIds.length, "miner").toLowerCase()} selected individually`
      : "";
  const placementText = placements.length > 0 ? `miners in ${placements.join(", ")}` : "";
  const detail =
    placementText && miners
      ? `Covers ${placementText}, plus ${miners}. Placement membership is evaluated live.`
      : placementText
        ? `Covers ${placementText}. Placement membership is evaluated live.`
        : `Covers ${miners}.`;
  return { title: "Applies to a subset of miners", detail };
};

const Section = ({ title, children }: { title: string; children: ReactNode }) => (
  <section className="grid gap-3">
    <div className="text-emphasis-300 text-text-primary">{title}</div>
    {children}
  </section>
);

interface SentenceRowProps {
  label: string;
  children: ReactNode;
}

const SentenceRow = ({ label, children }: SentenceRowProps) => (
  <div className="grid items-center gap-3 laptop:grid-cols-[minmax(8rem,1fr)_minmax(0,2fr)]">
    <div className="text-300 text-text-primary">{label}</div>
    <div className="grid grid-cols-2 gap-3">{children}</div>
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
  <SentenceRow label={rowLabel}>
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
  </SentenceRow>
);

interface AddRuleModalProps {
  open: boolean;
  editingRule: Rule | null;
  onDismiss: () => void;
}

const AddRuleModal = ({ open, editingRule, onDismiss }: AddRuleModalProps) => {
  const { createRule, updateRule, refresh } = useAlertsContext();
  // Delivery routing, applied with creation; edits keep the dedicated Edit delivery action.
  const routing = useDeliveryRouting();
  // Scope lives on the config, so it is editable on both create and edit.
  const scope = useAlertScope();
  // Rule writes only require alert:manage + miner:read; the pickers' list RPCs
  // need more, so gate them like ScheduleModal does to avoid permission-denied dead ends.
  const canReadSites = useHasPermission("site:read");
  const canReadRacks = useHasPermission("rack:read");
  const preferredTemperatureUnit: TemperatureFieldUnit = useTemperatureUnit() === "F" ? "°F" : "°C";

  const isEditing = editingRule != null;
  // A redacted scope has no device list to round-trip: rendering it as org-wide would
  // misreport coverage, and a save would drop the rule's explicit miners. Block editing.
  const scopeRedacted = isEditing && (editingRule?.config?.scope?.device_ids_redacted ?? false);
  // An interrupted save can leave the saved definition (which seeds this form) behind
  // what the rule actually evaluates; saving applies exactly what the form shows.
  const configOutOfSync = isEditing && (editingRule?.config_out_of_sync ?? false);

  type ScopePicker = "sites" | "buildings" | "racks" | "groups" | "miners";
  const [openPicker, setOpenPicker] = useState<ScopePicker | null>(null);

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
      scope.reset(cfg?.scope ?? null);
      setOpenPicker(null);
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

  // Mirror the server caps so the user gets actionable copy instead of the raw validation error.
  const validateScopeCaps = (): string | null => {
    for (const [ids, noun] of [
      [scope.siteIds, "sites"],
      [scope.buildingIds, "buildings"],
      [scope.rackIds, "racks"],
      [scope.groupIds, "groups"],
    ] as const) {
      if (ids.length > MAX_SCOPE_PLACEMENT) return `Rules can target at most ${MAX_SCOPE_PLACEMENT} ${noun}`;
    }
    if (scope.deviceIds.length > MAX_SCOPE_MINERS) {
      return `Rules can target at most ${MAX_SCOPE_MINERS} individually selected miners`;
    }
    return null;
  };

  const handleSave = async () => {
    const config = buildConfig();
    if (!config) return;
    const capError = validateScopeCaps();
    if (capError) {
      setErrorMsg(capError);
      return;
    }
    const ruleScope = scope.toRuleScope();
    if (ruleScope) config.scope = ruleScope;
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
      // A failed save can still have mutated server state (compensating pause/delete
      // after a version-skewed write); refetch rather than render the pre-save rule.
      void refresh();
    }
  };

  const summary = triggerSummary(template, amount, unit, durationSeconds);
  const applied = scopeSummary(scope);
  const sitesValue = scope.allSites ? "All sites" : getTargetButtonLabel(scope.siteIds.length, "site");
  const preview = useScopeSampleMiners(open, scope);

  const sampleValue = (miner: MinerStateSnapshot): string => {
    if (template === "offline") {
      switch (miner.deviceStatus) {
        case DeviceStatus.OFFLINE:
          return "Offline";
        case DeviceStatus.INACTIVE:
          return "Inactive";
        default:
          return "Online";
      }
    }
    const measurements = getMinerMeasurement(miner, (m) => (template === "hashrate" ? m.hashrate : m.temperature));
    const latest = measurements ? getLatestMeasurementWithData(measurements) : undefined;
    if (!latest) return "—";
    // "value unit" with a space, matching MinerMeasurement's fleet-table rendering.
    if (template === "hashrate") return `${getDisplayValue(latest.value)} TH/s`;
    const value = preferredTemperatureUnit === "°F" ? convertCtoF(latest.value) : latest.value;
    return `${getDisplayValue(value)} ${preferredTemperatureUnit}`;
  };

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={isEditing ? "Edit alert" : "Create an alert"}
        closeAriaLabel={isEditing ? "Close alert editor" : "Close alert creator"}
        onDismiss={onDismiss}
        isBusy={saving}
        buttons={[
          {
            text: "Save",
            variant: variants.primary,
            onClick: () => {
              void handleSave();
            },
            disabled: saving || scopeRedacted,
            loading: saving,
          },
        ]}
        primaryPane={
          <section className="flex flex-col gap-10 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            {errorMsg ? <Callout intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}
            {scopeRedacted ? (
              <Callout
                intent="danger"
                prefixIcon={<Alert />}
                title="This rule targets specific miners you don't have access to view, so it can't be edited. Ask for organization-wide miner access."
              />
            ) : null}
            {configOutOfSync ? (
              <Callout
                intent="danger"
                prefixIcon={<Alert />}
                title="An earlier save was interrupted, so this form may not match what the rule currently evaluates. Review every field — saving applies exactly what is shown here."
              />
            ) : null}

            <Section title="Details">
              <div className="grid grid-cols-2 gap-3">
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
                  label="Type"
                  options={TEMPLATE_OPTIONS}
                  value={template}
                  forceBelow
                  onChange={(value) => handleTemplateChange(value as UserRuleTemplate)}
                />
              </div>
            </Section>

            <Section title={CONDITION_SECTION_TITLE[template]}>
              <div className="grid gap-3">
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
                      // Scale changes (°C↔°F, TH/s↔PH/s) convert the amount so the threshold
                      // keeps its meaning; %↔absolute is a mode change and keeps the number.
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
              </div>
            </Section>

            {!isEditing ? (
              <Section title="Then send notification to">
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
              </Section>
            ) : null}

            <Section title="Apply to">
              <div className="grid">
                {canReadSites ? (
                  <TargetSelectButton label="Sites" value={sitesValue} onClick={() => setOpenPicker("sites")} />
                ) : null}
                {canReadSites ? (
                  <TargetSelectButton
                    label="Buildings"
                    value={getTargetButtonLabel(scope.buildingIds.length, "building")}
                    onClick={() => setOpenPicker("buildings")}
                  />
                ) : null}
                {canReadRacks ? (
                  <TargetSelectButton
                    label="Racks"
                    value={getTargetButtonLabel(scope.rackIds.length, "rack")}
                    onClick={() => setOpenPicker("racks")}
                  />
                ) : null}
                {canReadRacks ? (
                  <TargetSelectButton
                    label="Groups"
                    value={getTargetButtonLabel(scope.groupIds.length, "group")}
                    onClick={() => setOpenPicker("groups")}
                  />
                ) : null}
                <TargetSelectButton
                  label="Miners"
                  value={
                    scopeRedacted
                      ? "Restricted miners"
                      : scope.isOrgWide
                        ? "All miners"
                        : getTargetButtonLabel(scope.deviceIds.length, "miner")
                  }
                  onClick={() => setOpenPicker("miners")}
                />
              </div>
            </Section>
          </section>
        }
        secondaryPane={
          <div className="flex min-h-[360px] flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-8 py-12 laptop:min-h-0 laptop:px-16 laptop:py-6">
            <div className="flex w-full max-w-[620px] flex-col gap-10">
              <p className="text-heading-200 text-text-primary">{summary}</p>
              <div className="grid gap-1">
                <p className="text-emphasis-300 text-text-primary">{applied.title}</p>
                <p className="text-300 text-text-primary-70">{applied.detail}</p>
                {preview.total !== null ? (
                  <p className="text-300 text-text-primary-70">
                    {preview.total === 0
                      ? "No miners currently included"
                      : `${getTargetButtonLabel(preview.total, "miner")} currently included`}
                  </p>
                ) : null}
              </div>
              {preview.sample.length > 0 ? (
                <div className="flex flex-col">
                  {preview.sample.map((miner, index) => (
                    <div
                      key={miner.deviceIdentifier}
                      className={`flex h-12 items-center justify-between gap-4 ${
                        index < preview.sample.length - 1 ? "border-b border-border-5" : ""
                      }`}
                    >
                      <span className="min-w-0 truncate text-300 text-text-primary">
                        {miner.name || miner.deviceIdentifier}
                      </span>
                      <span className="text-300 whitespace-nowrap text-text-primary">{sampleValue(miner)}</span>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          </div>
        }
        secondaryPaneClassName="!bg-transparent laptop:!pl-0 laptop:!rounded-[24px]"
      />

      {openPicker === "sites" ? (
        <SiteSelectionModal
          open
          selectedSiteIds={scope.siteIds}
          allSitesSelected={scope.allSites}
          onDismiss={() => setOpenPicker(null)}
          onSave={({ siteIds, allSelected }) => {
            // "All sites" persists as the live flag so future sites are covered.
            scope.setAllSites(allSelected);
            scope.setSiteIds(allSelected ? [] : siteIds);
            setOpenPicker(null);
            clearError();
          }}
        />
      ) : null}

      {openPicker === "buildings" ? (
        <BuildingSelectionModal
          open
          selectedBuildingIds={scope.buildingIds}
          onDismiss={() => setOpenPicker(null)}
          onSave={(buildingIds) => {
            scope.setBuildingIds(buildingIds);
            setOpenPicker(null);
            clearError();
          }}
        />
      ) : null}

      {openPicker === "racks" ? (
        <RackSelectionModal
          open
          selectedRackIds={scope.rackIds}
          onDismiss={() => setOpenPicker(null)}
          onSave={(rackIds) => {
            scope.setRackIds(rackIds);
            setOpenPicker(null);
            clearError();
          }}
        />
      ) : null}

      {openPicker === "groups" ? (
        <GroupSelectionModal
          open
          selectedGroupIds={scope.groupIds}
          onDismiss={() => setOpenPicker(null)}
          onSave={(groupIds) => {
            scope.setGroupIds(groupIds);
            setOpenPicker(null);
            clearError();
          }}
        />
      ) : null}

      {openPicker === "miners" ? (
        <MinerSelectionModal
          open
          allMinersSelected={scope.isOrgWide}
          selectedMinerIds={scope.deviceIds}
          // The rack/group facets call ListDeviceSets (rack:read); hide them so
          // a miner:read-only manager can pick miners without permission-denied.
          filterConfig={canReadRacks ? undefined : { showRackFilter: false, showGroupFilter: false }}
          onDismiss={() => setOpenPicker(null)}
          onSave={(selection) => {
            if (selection.allSelected) {
              // A true "all miners" is org-wide: it supersedes every other dimension.
              scope.clearAll();
            } else {
              scope.setDeviceIds(selection.selectedMinerIds);
            }
            setOpenPicker(null);
            clearError();
          }}
        />
      ) : null}
    </>
  );
};

export default AddRuleModal;
