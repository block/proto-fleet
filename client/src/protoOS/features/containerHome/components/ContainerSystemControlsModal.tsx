import { useEffect, useRef, useState } from "react";
import { dryCoolerWriteHold } from "../hooks/useFanReversalOutcome";
import { useDryCoolerHold } from "../hooks/useFanReversalReadback";
import ContainerFanReversal from "./ContainerFanReversal";
import ContainerPlantAlarms from "./ContainerPlantAlarms";
import FanReversalOutcomeNotice from "./FanReversalOutcomeNotice";
import PlantAlarmOutcomeNotice from "./PlantAlarmOutcomeNotice";
import { BREAKER_OUTCOME_UNKNOWN } from "@/protoOS/api/adapters/breakerMessages";
import type { AdapterRequestOptions, ContainerSettings } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";
import { pushToast } from "@/shared/features/toaster";
import { convertCtoF, convertFtoC } from "@/shared/utils/telemetryFormat";
import { withDeadline } from "@/shared/utils/withDeadline";

const message = (error: unknown) =>
  error instanceof Error
    ? error.message
    : ((error as { error?: { message?: string } })?.error?.message ?? "The request did not complete.");
const isTemperature = (unit?: string) => unit === "C" || unit === "°C";
const MAIN_BREAKER = "main_circuit_breaker";
// The dedicated breaker is tracked with the boolean controls but never written through them.
const controlStates = (settings: ContainerSettings) => {
  const states = settings.controls.map((item): [string, boolean | undefined] => [item.name, item.enabled]);
  if (settings.mainCircuitBreaker) states.push([MAIN_BREAKER, settings.mainCircuitBreaker.closed]);
  return states;
};
// Plant alarms live beside the controls; a controller can offer them without ordinary plant controls.
const hasControlSurface = (settings: ContainerSettings) =>
  settings.supported ||
  settings.mainCircuitBreaker !== undefined ||
  settings.alarmMute === true ||
  settings.alarmReset === true ||
  settings.dryCoolerFanReversal !== undefined;
const isTimeout = (error: unknown) => error instanceof DOMException && error.name === "TimeoutError";
const setpointLabel = (name: string) => {
  if (name === "coolant_pump_speed_hz") return "Pump speed";
  if (name === "dry_cooler_manual_speed_percent") return "Fan speed";
  if (name === "dry_cooler_auto_temp_c") return "Target inlet temperature";
  return "Target temperature";
};

export default function ContainerSystemControlsModal({ onDismiss }: { onDismiss: () => void }) {
  const { adapter } = useMinerHosting();
  const [loaded, setLoaded] = useState<{ settings: ContainerSettings; requestedAtMs: number }>();
  const settings = loaded?.settings;
  const [error, setError] = useState<string>();
  const authRetry = useAuthRetry();
  useEffect(() => {
    const controller = new AbortController();
    const requestedAtMs = Date.now();
    if (adapter?.getContainerSettings)
      void authRetry({
        signal: controller.signal,
        request: (params) => adapter.getContainerSettings!(params),
        onSuccess: (value) => {
          if (!controller.signal.aborted) setLoaded({ settings: value, requestedAtMs });
        },
        onError: (error) => setError(`Could not load settings: ${message(error)}`),
      });
    return () => controller.abort();
  }, [adapter, authRetry]);
  if (loaded && hasControlSurface(loaded.settings))
    return (
      <SystemControlsForm
        settings={loaded.settings}
        settingsRequestedAtMs={loaded.requestedAtMs}
        onDismiss={onDismiss}
      />
    );
  return (
    <Modal open manageFocus title="System controls" dialogLabel="System controls" onDismiss={onDismiss}>
      <p role={error ? "alert" : "status"}>
        {error ??
          (settings || !adapter?.getContainerSettings
            ? "Controls are unavailable on this container."
            : "Loading settings…")}
      </p>
      {/* Without settings the Container alarms section cannot render, but its last result still matters. */}
      <PlantAlarmOutcomeNotice />
      <FanReversalOutcomeNotice />
    </Modal>
  );
}

function SystemControlsForm({
  settings,
  settingsRequestedAtMs,
  onDismiss,
}: {
  settings: ContainerSettings;
  settingsRequestedAtMs: number;
  onDismiss: () => void;
}) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const authRetry = useAuthRetry();
  const unit = useTemperatureUnit();
  const [controls, setControls] = useState<Record<string, boolean | undefined>>(() =>
    Object.fromEntries(controlStates(settings)),
  );
  const saved = useRef<Record<string, boolean | number | undefined>>(
    Object.fromEntries([...controlStates(settings), ...settings.setpoints.map((item) => [item.name, item.value])]),
  );
  const display = (value: number, settingUnit?: string) =>
    isTemperature(settingUnit) && unit === "F" ? convertCtoF(value) : value;
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      settings.setpoints.map((item) => [
        item.name,
        item.value === undefined ? "" : String(display(item.value, item.unit)),
      ]),
    ),
  );
  const [pending, setPending] = useState(false);
  const [controlPending, setControlPending] = useState(false);
  const [alarmBusy, setAlarmBusy] = useState(false);
  const [fanReversalBusy, setFanReversalBusy] = useState(false);
  // The PLC runs the reversal on the dry cooler; its settings stay as they are until it ends.
  const coolerHold = useDryCoolerHold();
  const coolerLocked = coolerHold !== undefined;
  const [powerConfirmation, setPowerConfirmation] = useState<{
    name: string;
    value: boolean;
    title: string;
    subtitle?: string;
    action: string;
    label: string;
    generation: number;
  }>();
  const [errors, setErrors] = useState<Record<string, string | undefined>>({});
  const dirty = useRef(new Set<string>());
  const queue = useRef<Promise<boolean>>(Promise.resolve(true));
  const pendingWrites = useRef(new Map<string, { value: boolean | number; result: Promise<boolean> }>());
  const pendingCount = useRef(0);
  const pendingControlCount = useRef(0);
  const requests = useRef(new Set<AbortController>());
  const needsRefresh = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    const activeRequests = requests.current;
    return () => {
      mounted.current = false;
      activeRequests.forEach((controller) => controller.abort());
    };
  }, []);
  const automatic = controls.dry_cooler_auto ?? saved.current.dry_cooler_auto;
  const coolerTarget =
    automatic === true ? "dry_cooler_auto_temp_c" : automatic === false ? "dry_cooler_manual_speed_percent" : undefined;
  const coolerSetpoints = settings.setpoints.filter((item) => item.name === coolerTarget);
  const hasControl = (name: string) => settings.controls.some((item) => item.name === name);
  const hasSetpoint = (name: string) => settings.setpoints.some((item) => item.name === name);
  const hasPump = hasControl("coolant_pump") || hasSetpoint("coolant_pump_speed_hz");
  const hasCooler = hasControl("dry_cooler") || hasControl("dry_cooler_auto") || coolerSetpoints.length > 0;
  const airConditioners = [1, 2].filter(
    (number) => hasControl(`air_conditioner_${number}`) || hasSetpoint(`air_conditioner_${number}_temp_c`),
  );
  const applyRead = (current: ContainerSettings) => {
    saved.current = Object.fromEntries([
      ...controlStates(current),
      ...current.setpoints.map((item) => [item.name, item.value]),
    ]);
    setControls(Object.fromEntries(controlStates(current)));
    setValues((previous) => ({
      ...previous,
      ...Object.fromEntries(
        current.setpoints
          .filter((item) => !dirty.current.has(item.name))
          .map((item) => [item.name, item.value === undefined ? "" : String(display(item.value, item.unit))]),
      ),
    }));
    needsRefresh.current = false;
  };
  // The PLC owned the dry cooler during the run, so show what it reads now. A change still being
  // saved re-reads on its own; without a usable read, the next change re-reads before writing.
  const fanReversalSettled = (current: ContainerSettings | undefined) => {
    if (!mounted.current) return;
    if (current && hasControlSurface(current) && pendingCount.current === 0) applyRead(current);
    else needsRefresh.current = true;
  };
  const matches = (name: string, value: boolean | number) => {
    const observed = saved.current[name];
    return typeof value === "number" && typeof observed === "number"
      ? Math.fround(observed) === Math.fround(value)
      : observed === value;
  };

  const save = (name: string, value: boolean | number, label: string, prerequisite?: Promise<boolean>) => {
    // Checked where the write is sent, so a confirmation opened before the hold cannot still send.
    const held = dryCoolerWriteHold(name);
    if (held) {
      setErrors((previous) => ({ ...previous, [name]: held }));
      return Promise.resolve(false);
    }
    const existing = pendingWrites.current.get(name);
    if (existing?.value === value) return existing.result;
    if (!needsRefresh.current && !existing && matches(name, value)) {
      dirty.current.delete(name);
      setErrors((previous) => ({ ...previous, [name]: undefined }));
      return Promise.resolve(true);
    }
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => mounted.current && useMinerStore.getState().connectionGeneration === generation;
    pendingCount.current++;
    setPending(true);
    if (typeof value === "boolean") {
      pendingControlCount.current++;
      setControlPending(true);
    }
    setErrors((previous) => ({ ...previous, [name]: undefined }));

    const request = async <T,>(run: (params: AdapterRequestOptions) => Promise<T>, timeoutMs = 15_000) => {
      const controller = new AbortController();
      requests.current.add(controller);
      let result: T | undefined;
      let failure: unknown;
      const canRequest = () => isCurrent() && !controller.signal.aborted;
      try {
        await withDeadline(
          () =>
            authRetry({
              signal: controller.signal,
              shouldRetry: canRequest,
              request: (params) => (canRequest() ? run(params) : Promise.resolve(undefined)),
              onSuccess: (value) => {
                if (canRequest()) result = value;
              },
              onError: (error) => {
                if (canRequest()) failure = error;
              },
            }),
          controller,
          timeoutMs,
        );
        if (failure) throw failure;
        return result;
      } finally {
        requests.current.delete(controller);
      }
    };
    const refreshSettings = async () => {
      try {
        const current = await request((params) => adapter!.getContainerSettings!(params));
        if (!isCurrent()) return;
        if (!current || !hasControlSurface(current)) throw new Error("Controls are unavailable on this container.");
        applyRead(current);
      } catch (error) {
        if (isCurrent()) {
          needsRefresh.current = true;
          setControls((previous) => Object.fromEntries(Object.keys(previous).map((key) => [key, undefined])));
        }
        throw Object.assign(new Error(`Could not refresh settings: ${message(error)}`), { cause: error });
      }
    };

    const result = queue.current
      .then(async () => {
        try {
          if (!isCurrent()) return false;
          if (prerequisite && !(await prerequisite)) throw new Error("Save the current setting before changing mode.");
          if (!isCurrent()) return false;
          if (needsRefresh.current) await refreshSettings();
          if (!isCurrent()) return false;
          if (!matches(name, value)) {
            let writeError: unknown;
            try {
              await request(
                (params) =>
                  typeof value !== "boolean"
                    ? adapter!.setContainerSetpoint!(name, value, params)
                    : name === MAIN_BREAKER
                      ? adapter!.setMainCircuitBreakerState!(value, params)
                      : adapter!.setContainerControl!(name, value, params),
                30_000,
              );
            } catch (error) {
              writeError =
                name === MAIN_BREAKER && isTimeout(error)
                  ? Object.assign(new Error(BREAKER_OUTCOME_UNKNOWN), { cause: error })
                  : error;
            }
            if (!isCurrent()) return false;
            // Read separately so a failed read can never replay the write. A failed read leaves
            // needsRefresh set, so the next change re-reads first instead of writing blind.
            let rereadError: unknown;
            try {
              await refreshSettings();
            } catch (error) {
              // Keep the breaker's write outcome visible; it matters most when the link drops.
              if (name !== MAIN_BREAKER) throw error;
              rereadError = error;
            }
            if (!isCurrent()) return false;
            if (writeError) {
              // A timed-out write may still have applied after this read.
              needsRefresh.current = true;
              if (!rereadError) throw writeError;
              throw Object.assign(new Error(`${message(writeError)} The current state could not be re-read.`), {
                cause: writeError,
              });
            }
            if (rereadError) {
              const detail = "The controller accepted the change, but the current state could not be re-read.";
              setErrors((previous) => ({ ...previous, [name]: `${detail} Check it before changing it again.` }));
              pushToast({ message: `${label}: ${detail}`, status: "error" });
              return true;
            }
            if (!matches(name, value)) throw new Error("The device did not confirm the requested value.");
          }
          if (typeof value === "number") {
            dirty.current.delete(name);
            const setting = settings.setpoints.find((item) => item.name === name);
            const observed = saved.current[name] as number;
            setValues((previous) => ({ ...previous, [name]: String(display(observed, setting?.unit)) }));
          }
          let refreshFailed = false;
          try {
            const accept = beginStoreWrite("telemetry:current:hashboard,miner");
            const telemetry = await request((params) => adapter!.getCurrentTelemetry(["miner", "hashboard"], params));
            if (isCurrent() && telemetry && accept())
              useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
          } catch {
            refreshFailed = true;
          }
          if (!isCurrent()) return false;
          const success =
            typeof value !== "boolean"
              ? `${label} saved.`
              : name === "dry_cooler_auto"
                ? `Mode set to ${value ? "Auto" : "Manual"}.`
                : name === MAIN_BREAKER
                  ? `${label} ${value ? "closed" : "opened"}.`
                  : `${label} turned ${value ? "on" : "off"}.`;
          pushToast({
            message: refreshFailed ? `${success} Live readings could not be refreshed.` : success,
            status: refreshFailed ? "error" : "success",
          });
          return true;
        } catch (error) {
          if (isCurrent()) {
            const detail = message(error);
            setErrors((previous) => ({ ...previous, [name]: detail }));
            pushToast({ message: `Could not save ${label.toLowerCase()}: ${detail}`, status: "error" });
          }
          return false;
        }
      })
      .finally(() => {
        if (pendingWrites.current.get(name)?.result === result) pendingWrites.current.delete(name);
        pendingCount.current--;
        if (typeof value === "boolean") pendingControlCount.current--;
        if (isCurrent()) {
          setPending(pendingCount.current > 0);
          setControlPending(pendingControlCount.current > 0);
        }
      });
    queue.current = result;
    pendingWrites.current.set(name, { value, result });
    return result;
  };

  const saveSetpoint = (
    item: ContainerSettings["setpoints"][number],
    text: string,
    label = setpointLabel(item.name),
  ) => {
    const value = isTemperature(item.unit) && unit === "F" ? convertFtoC(Number(text)) : Number(text);
    const invalid = !text.trim() || !Number.isFinite(Math.fround(value));
    const outOfRange = (item.min !== undefined && value < item.min) || (item.max !== undefined && value > item.max);
    if (invalid || outOfRange) {
      setErrors((previous) => ({
        ...previous,
        [item.name]: invalid ? "Enter a valid number." : "Enter a value within the documented range.",
      }));
      return Promise.resolve(false);
    }
    return save(item.name, value, label);
  };
  const saveControl = (name: string, value: boolean, label: string) => {
    const prerequisite =
      name === "dry_cooler_auto"
        ? Promise.all(
            coolerSetpoints
              .filter((item) => dirty.current.has(item.name))
              .map((item) => saveSetpoint(item, values[item.name])),
          ).then((results) => results.every(Boolean))
        : undefined;
    void save(name, value, label, prerequisite);
  };
  const powerRow = (name: string, equipment: string, label = "Power") =>
    hasControl(name) ? (
      <div>
        <div className="flex items-center justify-between gap-4">
          <span className="text-emphasis-300">{label}</span>
          <div className="flex items-center gap-4">
            {controls[name] === undefined ? <span className="text-text-primary-70">Unavailable</span> : null}
            <Switch
              ariaLabel={label === "Power" ? `${equipment} power` : label}
              checked={controls[name] === true}
              disabled={
                controlPending ||
                controls[name] === undefined ||
                !adapter?.setContainerControl ||
                (name === "dry_cooler" && coolerLocked)
              }
              setChecked={() =>
                setPowerConfirmation({
                  name,
                  value: !controls[name],
                  title: `${controls[name] ? "Turn off" : "Turn on"} ${equipment}?`,
                  action: controls[name] ? "Turn off" : "Turn on",
                  label: label === "Power" ? `${equipment} power` : label,
                  generation: useMinerStore.getState().connectionGeneration,
                })
              }
            />
          </div>
        </div>
        {errors[name] ? (
          <p role="alert" className="mt-2 text-200 text-text-critical">
            {errors[name]}
          </p>
        ) : null}
      </div>
    ) : null;
  const breakerClosed = controls[MAIN_BREAKER];
  const breakerRow = settings.mainCircuitBreaker ? (
    <section aria-label="Main circuit breaker controls" className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-heading-100">Main circuit breaker</h2>
          <p className="text-200 text-text-primary-70">Affects the whole container.</p>
          {isFleetHosted ? (
            <p className="text-200 text-text-primary-70">
              Changing the breaker needs a direct connection to the controller.
            </p>
          ) : null}
        </div>
        <div className="flex items-center gap-4">
          <span className="text-text-primary-70">
            {breakerClosed === undefined ? "Unavailable" : breakerClosed ? "Closed" : "Open"}
          </span>
          <Switch
            ariaLabel="Main circuit breaker closed"
            checked={breakerClosed === true}
            disabled={
              controlPending || breakerClosed === undefined || isFleetHosted || !adapter?.setMainCircuitBreakerState
            }
            setChecked={() =>
              setPowerConfirmation({
                name: MAIN_BREAKER,
                value: !breakerClosed,
                title: `${breakerClosed ? "Open" : "Close"} the main circuit breaker?`,
                subtitle: "This changes power for the whole container, not a single tank or module.",
                action: breakerClosed ? "Open breaker" : "Close breaker",
                label: "Main circuit breaker",
                generation: useMinerStore.getState().connectionGeneration,
              })
            }
          />
        </div>
      </div>
      {errors[MAIN_BREAKER] ? (
        <p role="alert" className="text-200 text-text-critical">
          {errors[MAIN_BREAKER]}
        </p>
      ) : null}
    </section>
  ) : null;
  const alarmsRow =
    settings.alarmMute || settings.alarmReset ? (
      <ContainerPlantAlarms settings={settings} onBusyChange={setAlarmBusy} />
    ) : null;
  // A confirmed procedure like the alarm commands, so it gets its own section, not a cooler setting.
  const fanReversalRow = settings.dryCoolerFanReversal ? (
    <ContainerFanReversal
      settings={settings}
      settingsRequestedAtMs={settingsRequestedAtMs}
      settingsPending={pending}
      dryCoolerAuto={controls.dry_cooler_auto}
      onSettled={fanReversalSettled}
      onBusyChange={setFanReversalBusy}
    />
  ) : null;
  const setpointField = (name: string, saveLabel?: string, locked = false) => {
    const item = settings.setpoints.find((setting) => setting.name === name);
    if (!item) return null;
    const label = setpointLabel(name);
    const suffix = isTemperature(item.unit) ? `°${unit}` : item.unit;
    return (
      <div key={name}>
        <Input
          id={`system-${name}`}
          label={`${label}${suffix ? ` (${suffix})` : ""}`}
          initValue={values[name]}
          inputMode="decimal"
          disabled={pending || locked || !adapter?.setContainerSetpoint}
          error={errors[name]}
          onChangeBlur={(value) => void saveSetpoint(item, value, saveLabel)}
          onKeyDown={(key) => {
            if (key === "Enter") void saveSetpoint(item, values[name], saveLabel);
          }}
          onChange={(value) => {
            dirty.current.add(name);
            setValues((previous) => ({ ...previous, [name]: value }));
            setErrors((previous) => ({ ...previous, [name]: undefined }));
          }}
        />
        {item.min !== undefined && item.max !== undefined ? (
          <p className="mt-2 text-200 text-text-primary-70">
            Documented range: {display(item.min, item.unit)}–{display(item.max, item.unit)} {suffix}
          </p>
        ) : null}
      </div>
    );
  };
  return (
    <>
      <Modal
        open
        manageFocus
        title="System controls"
        dialogLabel="System controls"
        onDismiss={pending || alarmBusy || fanReversalBusy || powerConfirmation ? undefined : onDismiss}
      >
        <div className="space-y-6" aria-busy={pending}>
          <p className="text-300 text-text-primary-50">Changes save automatically.</p>
          {breakerRow}
          {breakerRow && alarmsRow ? <Divider /> : null}
          {alarmsRow}
          {(breakerRow || alarmsRow) &&
          (hasPump || hasCooler || fanReversalRow || airConditioners.length || hasControl("tank_led_light")) ? (
            <Divider />
          ) : null}
          {hasPump || hasCooler ? (
            <div className="grid grid-cols-1 gap-6 tablet:grid-cols-2">
              {hasPump ? (
                <section aria-label="Coolant pump controls" className="min-w-0 space-y-4">
                  <h2 className="text-heading-100">Coolant pump</h2>
                  {powerRow("coolant_pump", "Coolant pump")}
                  {setpointField("coolant_pump_speed_hz")}
                </section>
              ) : null}
              {hasCooler ? (
                <section aria-label="Dry cooler controls" className="min-w-0 space-y-4">
                  <h2 className="text-heading-100">Dry cooler</h2>
                  {powerRow("dry_cooler", "Dry cooler")}
                  {hasControl("dry_cooler_auto") ? (
                    <Select
                      id="system-dry-cooler-mode"
                      label="Mode"
                      options={[
                        { value: "auto", label: "Auto" },
                        { value: "manual", label: "Manual" },
                      ]}
                      value={controls.dry_cooler_auto === undefined ? "" : controls.dry_cooler_auto ? "auto" : "manual"}
                      placeholder="Select mode"
                      disabled={controlPending || coolerLocked || !adapter?.setContainerControl}
                      error={errors.dry_cooler_auto}
                      onChange={(value) => saveControl("dry_cooler_auto", value === "auto", "Mode")}
                    />
                  ) : null}
                  {coolerTarget ? setpointField(coolerTarget, undefined, coolerLocked) : null}
                  {coolerLocked && (hasControl("dry_cooler") || hasControl("dry_cooler_auto") || coolerTarget) ? (
                    <p className="text-200 text-text-primary-70">{coolerHold}</p>
                  ) : null}
                </section>
              ) : null}
            </div>
          ) : null}
          {fanReversalRow ? (
            <>
              {hasPump || hasCooler ? <Divider /> : null}
              {fanReversalRow}
            </>
          ) : null}
          {airConditioners.length ? (
            <>
              {hasPump || hasCooler || fanReversalRow ? <Divider /> : null}
              <div className="grid grid-cols-1 gap-6 tablet:grid-cols-2">
                {airConditioners.map((number) => (
                  <section key={number} aria-label={`Air conditioner ${number} controls`} className="min-w-0 space-y-4">
                    <h2 className="text-heading-100">Air conditioner {number}</h2>
                    {powerRow(`air_conditioner_${number}`, `Air conditioner ${number}`)}
                    {setpointField(`air_conditioner_${number}_temp_c`, `Air conditioner ${number} target temperature`)}
                  </section>
                ))}
              </div>
            </>
          ) : null}
          {hasControl("tank_led_light") ? (
            <>
              {hasPump || hasCooler || fanReversalRow || airConditioners.length ? <Divider /> : null}
              <section aria-label="Lighting controls">
                {powerRow("tank_led_light", "Tank lights", "Tank lights")}
              </section>
            </>
          ) : null}
          {!breakerRow &&
          !alarmsRow &&
          !hasPump &&
          !hasCooler &&
          !fanReversalRow &&
          !airConditioners.length &&
          !hasControl("tank_led_light") ? (
            <p>No controls are available on this container.</p>
          ) : null}
        </div>
      </Modal>
      {powerConfirmation ? (
        <Dialog
          title={powerConfirmation.title}
          subtitle={powerConfirmation.subtitle}
          manageFocus
          preventScroll
          onDismiss={() => setPowerConfirmation(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setPowerConfirmation(undefined) },
            {
              text: powerConfirmation.action,
              variant:
                powerConfirmation.name === MAIN_BREAKER && !powerConfirmation.value
                  ? variants.danger
                  : variants.primary,
              disabled: controlPending || (coolerLocked && powerConfirmation.name.startsWith("dry_cooler")),
              onClick: () => {
                setPowerConfirmation(undefined);
                if (useMinerStore.getState().connectionGeneration === powerConfirmation.generation) {
                  saveControl(powerConfirmation.name, powerConfirmation.value, powerConfirmation.label);
                }
              },
            },
          ]}
        />
      ) : null}
    </>
  );
}
