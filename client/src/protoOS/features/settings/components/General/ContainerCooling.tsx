import { useCallback, useEffect, useRef, useState } from "react";
import type { ContainerSettings } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useTemperatureUnit } from "@/protoOS/store";
import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import { convertCtoF, convertFtoC } from "@/shared/utils/telemetryFormat";

const controlLabels = {
  coolant_pump: "Coolant pump",
  dry_cooler: "Dry cooler",
  dry_cooler_auto: "Dry cooler mode",
  tank_led_light: "Tank lights",
  air_conditioner_1: "Air conditioner 1",
  air_conditioner_2: "Air conditioner 2",
};

const setpointLabels = {
  coolant_pump_speed_hz: "Coolant pump speed",
  dry_cooler_auto_temp_c: "Dry cooler target temperature",
  dry_cooler_manual_speed_percent: "Dry cooler manual speed",
  air_conditioner_1_temp_c: "Air conditioner 1 target temperature",
  air_conditioner_2_temp_c: "Air conditioner 2 target temperature",
};

const labels: Record<string, string> = { ...controlLabels, ...setpointLabels };
const unavailableControls = Object.keys(controlLabels).map((name) => ({ name }));
const unavailableSetpoints = Object.keys(setpointLabels).map((name) => ({ name }));
const labelFor = (name: string) => labels[name] ?? name.replaceAll("_", " ");
const stateLabel = (name: string, enabled: boolean) =>
  name === "dry_cooler_auto" ? (enabled ? "Auto" : "Manual") : enabled ? "On" : "Off";
const isTemperature = (unit?: string) => unit === "C" || unit === "°C";
type Selection =
  | { kind: "control"; setting: ContainerSettings["controls"][number] }
  | { kind: "setpoint"; setting: ContainerSettings["setpoints"][number] };

const ContainerCooling = () => {
  const { adapter } = useMinerHosting();
  const temperatureUnit = useTemperatureUnit();
  const displayValue = (value: number, unit?: string) =>
    isTemperature(unit) && temperatureUnit === "F" ? convertCtoF(value) : value;
  const displayUnit = (unit?: string) => (isTemperature(unit) ? `°${temperatureUnit}` : unit);
  const [settings, setSettings] = useState<ContainerSettings>();
  const [selection, setSelection] = useState<Selection>();
  const [value, setValue] = useState("");
  const [enabled, setEnabled] = useState<boolean>();
  const [error, setError] = useState<string>();
  const [unavailable, setUnavailable] = useState(false);
  const [busy, setBusy] = useState(false);
  const writing = useRef(false);
  const controls: ContainerSettings["controls"] = settings?.supported ? settings.controls : unavailableControls;
  const setpoints: ContainerSettings["setpoints"] = settings?.supported ? settings.setpoints : unavailableSetpoints;
  const canEdit = settings?.supported === true && !unavailable;

  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      if (!adapter?.getContainerSettings) return;
      try {
        const current = await adapter.getContainerSettings({ signal });
        if (signal?.aborted) return;
        setSettings(current);
        setUnavailable(false);
      } catch {
        if (signal?.aborted) return;
        setUnavailable(true);
      }
    },
    [adapter],
  );

  useEffect(() => {
    const controller = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- refresh writes state only after the asynchronous device read completes
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  const edit = (selection: Selection) => {
    setSelection(selection);
    setError(undefined);
    setEnabled(selection.kind === "control" ? selection.setting.enabled : undefined);
    setValue(
      selection.kind === "setpoint" && selection.setting.value !== undefined
        ? String(displayValue(selection.setting.value, selection.setting.unit))
        : "",
    );
  };

  const save = async () => {
    if (!selection || writing.current || unavailable) return;
    writing.current = true;
    setBusy(true);
    setError(undefined);
    try {
      if (selection.kind === "control") {
        if (!adapter?.setContainerControl || enabled === undefined) throw new Error("Select a state");
        await adapter.setContainerControl(selection.setting.name, enabled);
      } else {
        if (!adapter?.setContainerSetpoint || !value.trim() || !Number.isFinite(Number(value)))
          throw new Error("Enter a number");
        const wireValue =
          isTemperature(selection.setting.unit) && temperatureUnit === "F" ? convertFtoC(Number(value)) : Number(value);
        await adapter.setContainerSetpoint(selection.setting.name, wireValue);
      }
      setSelection(undefined);
      await refresh();
    } catch {
      setError("Could not apply this change.");
      await refresh();
    } finally {
      writing.current = false;
      setBusy(false);
    }
  };

  return (
    <section className="mb-10" aria-label="Container controls">
      <h3 className="mb-2 text-heading-100">Cooling and container controls</h3>
      <>
        {controls.map((setting) => (
          <Row key={setting.name} className="flex items-center justify-between gap-4">
            <h4>{labelFor(setting.name)}</h4>
            <div className="flex items-center gap-4">
              <span>
                {unavailable || setting.enabled === undefined ? "—" : stateLabel(setting.name, setting.enabled)}
              </span>
              {canEdit ? (
                <Button
                  variant="secondary"
                  size="compact"
                  text="Edit"
                  ariaLabel={`Edit ${labelFor(setting.name)}`}
                  disabled={unavailable || busy}
                  onClick={() => edit({ kind: "control", setting })}
                />
              ) : null}
            </div>
          </Row>
        ))}
        {setpoints.map((setting) => (
          <Row key={setting.name} className="flex items-center justify-between gap-4">
            <h4>{labelFor(setting.name)}</h4>
            <div className="flex items-center gap-4">
              <span>
                {unavailable || setting.value === undefined
                  ? "—"
                  : `${displayValue(setting.value, setting.unit)} ${displayUnit(setting.unit) ?? ""}`}
              </span>
              {canEdit ? (
                <Button
                  variant="secondary"
                  size="compact"
                  text="Edit"
                  ariaLabel={`Edit ${labelFor(setting.name)}`}
                  disabled={unavailable || busy}
                  onClick={() => edit({ kind: "setpoint", setting })}
                />
              ) : null}
            </div>
          </Row>
        ))}
      </>
      {selection ? (
        <Modal
          open
          title={labelFor(selection.setting.name)}
          dialogLabel={labelFor(selection.setting.name)}
          manageFocus
          onDismiss={() => {
            if (!busy) setSelection(undefined);
          }}
          buttons={[
            {
              text: "Apply",
              variant: "primary",
              loading: busy,
              dismissModalOnClick: false,
              onClick: () => {
                void save();
              },
              disabled:
                !canEdit ||
                (selection.kind === "control"
                  ? enabled === undefined
                  : !value.trim() || !Number.isFinite(Number(value)) || !Number.isFinite(Math.fround(Number(value)))),
            },
          ]}
        >
          {error ? (
            <p role="alert" className="mb-4 text-intent-critical-fill">
              {error}
            </p>
          ) : null}
          {selection.kind === "control" ? (
            <div className="flex gap-3" role="group" aria-label="Control state">
              <Button
                variant={enabled === true ? "primary" : "secondary"}
                text={stateLabel(selection.setting.name, true)}
                ariaPressed={enabled === true}
                disabled={busy}
                onClick={() => setEnabled(true)}
              />
              <Button
                variant={enabled === false ? "primary" : "secondary"}
                text={stateLabel(selection.setting.name, false)}
                ariaPressed={enabled === false}
                disabled={busy}
                onClick={() => setEnabled(false)}
              />
            </div>
          ) : (
            <>
              <Input
                id="container-setpoint"
                label={`${labelFor(selection.setting.name)}${selection.setting.unit ? ` (${displayUnit(selection.setting.unit)})` : ""}`}
                initValue={value}
                onChange={setValue}
                disabled={busy}
              />
              {selection.setting.min !== undefined && selection.setting.max !== undefined ? (
                <p className="mt-3 text-200 text-text-primary-70">
                  Range: {displayValue(selection.setting.min, selection.setting.unit)}–
                  {displayValue(selection.setting.max, selection.setting.unit)} {displayUnit(selection.setting.unit)}
                </p>
              ) : null}
            </>
          )}
        </Modal>
      ) : null}
    </section>
  );
};

export default ContainerCooling;
