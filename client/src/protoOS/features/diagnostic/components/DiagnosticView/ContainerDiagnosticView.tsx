import { type ReactNode, useMemo, useState } from "react";
import clsx from "clsx";

import DiagnosticStatusModal from "./DiagnosticStatusModal";
import DryCoolerSettingsModal from "./DryCoolerSettingsModal";
import { useContainerControl } from "./useContainerControl";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useScopeHealth } from "@/protoOS/components/StatusModal/useScopeHealth";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { tankSide } from "@/protoOS/features/tanks/tankSide";
import { hashboardAttentionFill } from "@/protoOS/features/tanks/tankStatusColors";
import {
  boardStatus,
  hashboardLabel,
  moduleHashboards,
  statusLabels,
  tankGrid,
  tankMiningScope,
  tankModules,
} from "@/protoOS/features/tanks/tankView";
import { useTankPower } from "@/protoOS/features/tanks/useTankPower";
import {
  asEntityKey,
  type ContainerTopology,
  type HealthState,
  type LevelRollup,
  type MetricTelemetry,
  type MiningStatus,
  type PlantTelemetry,
  type TankNode,
} from "@/protoOS/store/contract";
import { useFlowUnit, useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { Alert, Info } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import RadialGauge from "@/shared/components/RadialGauge";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Switch from "@/shared/components/Switch";
import type { TemperatureUnit } from "@/shared/features/preferences/types";
import { formatFlowRate } from "@/shared/utils/flowRate";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

type DiagnosticTab = "all" | "tanks" | "dry-cooler" | "power" | "controls";

// Keep the telemetry subscription out of the view so polling state alone does
// not re-render every card. Remount when scope changes to cancel the old poll.
const DiagnosticsPolling = ({ full }: { full: boolean }) => {
  const level = useMemo<TelemetryLevel[]>(() => (full ? ["hashboard", "miner"] : ["miner"]), [full]);
  const { error } = useTelemetry({ level });
  return error ? (
    <p role="alert" className="mb-6 text-300 text-text-critical">
      Could not refresh container readings. Displayed values may be out of date.
    </p>
  ) : null;
};

const tabs = [
  { key: "all", title: "All" },
  { key: "tanks", title: "Tanks" },
  { key: "dry-cooler", title: "Cooling" },
  { key: "power", title: "Power" },
  { key: "controls", title: "Controls" },
];

const PASCALS_PER_PSI = 6_894.757;

const latest = (metric: MetricTelemetry | undefined) => {
  const value = metric?.latest?.value;
  return value !== null && value !== undefined && Number.isFinite(value) ? value : undefined;
};

const decimal = (value: number | undefined, digits = 1) =>
  value === undefined
    ? "—"
    : value.toLocaleString(undefined, { maximumFractionDigits: digits, minimumFractionDigits: digits });

const formatTemperature = (value: number | undefined, unit: TemperatureUnit) => {
  if (value === undefined) return "—";
  const converted = unit === "F" ? convertCtoF(value) : value;
  return `${decimal(converted)}°${unit}`;
};

const formatPressure = (pascals: number | undefined) =>
  pascals === undefined ? "—" : `${decimal(pascals / PASCALS_PER_PSI)} psi`;

const Stat = ({ label, value }: { label: string; value: string }) => (
  <div>
    <dt className="mb-1 text-300 text-text-primary-70">{label}</dt>
    <dd className="text-emphasis-400 text-text-primary tabular-nums">{value}</dd>
  </div>
);

const Panel = ({ children, className }: { children: ReactNode; className?: string }) => (
  <div className={clsx("rounded-2xl bg-surface-5 p-6", className)}>{children}</div>
);

const useContainerDiagnostics = () => {
  const topology = useMinerStore((state) => state.hardware.topology);
  const plantHardware = useMinerStore((state) => state.hardware.plant);
  const plantTelemetry = useMinerStore((state) => state.telemetry.plant);
  const fanHardware = useMinerStore((state) => state.hardware.fans);
  const fanTelemetry = useMinerStore((state) => state.telemetry.fans);
  const rollups = useMinerStore((state) => state.telemetry.rollupsByEntity);

  return useMemo(() => {
    if (topology?.kind !== "container") return undefined;
    const plantEntry = [...plantHardware.entries()].find(([, hardware]) => hardware.kind === "plant");
    const plant = plantEntry ? plantTelemetry.get(plantEntry[0]) : undefined;
    const tanks = [...topology.tanks.values()].sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity));
    const fans = [...fanHardware.entries()].map(([id, hardware], index) => {
      const number = hardware.name?.match(/(\d+)$/)?.[1] ?? index + 1;
      return { id, name: `Fan ${number}`, telemetry: fanTelemetry.get(id) };
    });
    const powerComponents = [...plantHardware.entries()]
      .filter(([, hardware]) => hardware.kind === "distribution")
      .map(([id, hardware]) => ({ hardware, telemetry: plantTelemetry.get(id) }));
    return {
      topology,
      plant,
      tanks,
      fans,
      powerComponents,
      rollups,
      containerRollup: rollups.get(asEntityKey(topology.containerId)),
    };
  }, [fanHardware, fanTelemetry, plantHardware, plantTelemetry, rollups, topology]);
};

const ContainerSummary = ({ plant, rollup }: { plant?: PlantTelemetry; rollup?: LevelRollup }) => {
  const unit = useTemperatureUnit();
  const temperatureDelta = (value: number) => `${decimal(unit === "F" ? (value * 9) / 5 : value)}°${unit}`;
  const inlet = latest(plant?.inletTemperature);
  const outlet = latest(plant?.outletTemperature);
  const pumpInlet = latest(plant?.pumpInletPressure);
  const pumpOutlet = latest(plant?.pumpOutletPressure);
  const totalPower = latest(plant?.totalPower);
  const state = rollup?.miningStatus;
  const stateLabel =
    state === "mining" || state === "degradedMining" ? "Mining" : state && state !== "unknown" ? "Not mining" : "—";

  return (
    <div className="grid gap-1 tablet:grid-cols-2">
      <Panel>
        <dl className="grid grid-cols-2 gap-x-8 gap-y-8">
          <Stat label="State" value={stateLabel} />
          <Stat label="Total power" value={totalPower === undefined ? "—" : `${decimal(totalPower / 1_000)} kW`} />
          <Stat label="Ambient temperature" value={formatTemperature(latest(plant?.ambientTemperature), unit)} />
          <Stat
            label="Cooler"
            value={plant?.heatRejectionActive === undefined ? "—" : plant.heatRejectionActive ? "Active" : "Inactive"}
          />
        </dl>
      </Panel>
      <Panel>
        <dl className="grid grid-cols-2 gap-x-8 gap-y-8">
          <Stat
            label="Inlet to outlet temperature"
            value={
              inlet === undefined || outlet === undefined
                ? "—"
                : `${formatTemperature(inlet, unit)} → ${formatTemperature(outlet, unit)}`
            }
          />
          <Stat
            label="Inlet to outlet pressure"
            value={
              pumpInlet === undefined || pumpOutlet === undefined
                ? "—"
                : `${formatPressure(pumpInlet)} → ${formatPressure(pumpOutlet)}`
            }
          />
          <Stat
            label="Temperature delta"
            value={inlet === undefined || outlet === undefined ? "—" : temperatureDelta(outlet - inlet)}
          />
          <Stat
            label="Pressure delta"
            value={pumpInlet === undefined || pumpOutlet === undefined ? "—" : formatPressure(pumpOutlet - pumpInlet)}
          />
        </dl>
      </Panel>
    </div>
  );
};

const boardColor = {
  isolated: "bg-text-primary-30",
  offline: "bg-surface-10",
  sleeping: "bg-surface-10",
  healthy: "bg-surface-10",
  unknown: "border border-border-10 bg-surface-base",
};

const healthLabels: Record<HealthState, string> = {
  ok: "Healthy",
  degraded: "Needs attention",
  fault: "Fault",
  offline: "Offline",
  unknown: "Unknown",
};

type TankPower = ReturnType<typeof useTankPower>;

const tankPowerState = (status: MiningStatus | undefined) => ({
  on: status === "mining" || status === "degradedMining" || status === "poweringOn",
  actionable: status !== undefined && status !== "unknown" && status !== "poweringOn" && status !== "poweringOff",
});

const InfoButton = ({ label, onClick }: { label: string; onClick: () => void }) => (
  <button
    type="button"
    aria-label={label}
    aria-haspopup="dialog"
    onClick={onClick}
    className="rounded-full bg-surface-10 p-1 text-text-primary-70 hover:bg-surface-20 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
  >
    <Info />
  </button>
);

const TankDiagnosticCard = ({
  tank,
  topology,
  powerControl,
}: {
  tank: TankNode;
  topology: ContainerTopology;
  powerControl: TankPower;
}) => {
  const unit = useTemperatureUnit();
  const [showInfo, setShowInfo] = useState(false);
  const rollup = useMinerStore((state) => state.telemetry.rollupsByEntity.get(asEntityKey(tank.id)));
  const telemetry = useMinerStore((state) => state.telemetry.hashboards);
  const modules = useMemo(() => tankModules(tank, topology), [tank, topology]);
  const grid = useMemo(() => tankGrid(tank, modules), [tank, modules]);
  const scope = useMemo(() => tankMiningScope(tank, modules), [tank, modules]);
  const health = useScopeHealth({ kind: "tank", id: tank.id });
  const hasIssue = health?.needsAttention ?? false;
  const { on, actionable } = tankPowerState(rollup?.miningStatus);
  const boardCount = rollup?.boardCount;
  const onlineBoardCount = rollup?.onlineBoardCount;
  const boardAvailability =
    boardCount === undefined || onlineBoardCount === undefined ? "—" : `${onlineBoardCount}/${boardCount}`;
  const temperatureText = formatTemperature(latest(rollup?.averageAsicTemperature), unit);
  const power = latest(rollup?.power);
  const powerText = power === undefined ? "—" : `${decimal(power / 1_000)} kW`;

  return (
    <article className="rounded-2xl bg-surface-5 p-6" aria-label={tank.label ?? `Tank ${tank.slot ?? ""}`}>
      <div className="mb-5 flex items-center justify-between gap-3">
        <h3 className="flex items-center gap-2 text-emphasis-300">
          {hasIssue ? <Alert className="text-intent-critical-fill" /> : null}
          {tank.label ?? `Tank ${tank.slot ?? ""}`}
        </h3>
        <div className="flex items-center gap-3">
          <Switch
            ariaLabel={`${tank.label ?? "Tank"} power`}
            checked={on}
            disabled={
              !scope ||
              !actionable ||
              tank.capabilities.startStop !== "supported" ||
              !powerControl.supported ||
              powerControl.pending
            }
            setChecked={() => void powerControl.setPower(scope, !on)}
          />
          <InfoButton label={`${tank.label ?? "Tank"} info`} onClick={() => setShowInfo(true)} />
        </div>
      </div>
      <div
        className="mb-5 grid gap-3"
        aria-label="Module health"
        style={{
          gridTemplateColumns: `repeat(${grid.columns}, minmax(0, 1fr))`,
          gridTemplateRows: `repeat(${grid.rows}, 56px)`,
        }}
      >
        {modules.map((module, index) => {
          const bySlot = new Map(moduleHashboards(module, topology).map((board) => [board.location?.slot, board]));
          const position = grid.positions[index];
          const minerStatus = health?.moduleStatuses.get(module.id) ?? "unknown";
          const needsModuleWarning =
            minerStatus === "attention" &&
            !module.hashboardIds.some((id) => boardStatus(telemetry.get(id)) === "attention");
          return (
            <span
              key={module.id}
              role="group"
              aria-label={`Module ${module.location?.moduleSlot ?? module.id}`}
              className="relative flex h-14 overflow-hidden rounded-sm"
              style={position ? { gridColumn: position.column, gridRow: position.row } : undefined}
            >
              {[1, 2, 3].map((slot) => {
                const board = bySlot.get(slot);
                const reading = board ? telemetry.get(board.id) : undefined;
                const status = boardStatus(reading);
                const count = reading?.reportedIssueCount;
                const label = `${hashboardLabel(slot)} hashboard: ${statusLabels[status]}${
                  count ? `, ${count} reported ${count === 1 ? "issue" : "issues"}` : ""
                }`;
                return (
                  <span
                    key={slot}
                    role="img"
                    aria-label={label}
                    title={label}
                    className={clsx(
                      "h-full min-w-0 flex-1",
                      status === "attention" ? hashboardAttentionFill(count) : boardColor[status],
                    )}
                  />
                );
              })}
              {needsModuleWarning ? (
                <span
                  role="img"
                  aria-label={statusLabels.attention}
                  title={statusLabels.attention}
                  className="absolute inset-0 flex items-center justify-center"
                >
                  <Alert width="w-2/3" className="text-intent-critical-fill" />
                </span>
              ) : null}
              {minerStatus === "offline" || minerStatus === "sleeping" ? (
                <span
                  role="img"
                  aria-label={statusLabels[minerStatus]}
                  title={statusLabels[minerStatus]}
                  className={clsx(
                    "absolute top-1/2 left-1/2 aspect-square w-[45%] -translate-x-1/2 -translate-y-1/2 rounded-full",
                    minerStatus === "offline" ? "bg-core-accent-fill" : "bg-text-primary-30",
                  )}
                />
              ) : null}
            </span>
          );
        })}
      </div>
      <dl className="grid grid-cols-3 gap-4 text-center text-300 text-text-primary-70 tabular-nums">
        <div>
          <dt className="sr-only">Online hashboards</dt>
          <dd>{boardAvailability}</dd>
        </div>
        <div>
          <dt className="sr-only">Temperature</dt>
          <dd>{temperatureText}</dd>
        </div>
        <div>
          <dt className="sr-only">Power</dt>
          <dd>{powerText}</dd>
        </div>
      </dl>
      {showInfo ? (
        <ProtoOSStatusModal scope={{ kind: "tank", id: tank.id }} onClose={() => setShowInfo(false)} />
      ) : null}
    </article>
  );
};

const TankBank = ({
  side,
  tanks,
  data,
  powerControl,
}: {
  side: "Left" | "Right";
  tanks: TankNode[];
  data: NonNullable<ReturnType<typeof useContainerDiagnostics>>;
  powerControl: TankPower;
}) => {
  const plant = data.plant;
  const unit = useTemperatureUnit();
  const flow = latest(plant?.flowRate);
  const flowUnit = useFlowUnit();
  const outlet = side === "Left" ? plant?.outletATemperature : plant?.outletBTemperature;
  const liquidLevel = latest(side === "Left" ? plant?.tankALiquidLevel : plant?.tankBLiquidLevel);
  const scopes = tanks.map((tank) => tankMiningScope(tank, tankModules(tank, data.topology)));
  const scope =
    scopes.length && scopes.every((item) => item !== undefined)
      ? { modules: scopes.flatMap((item) => item!.modules ?? []) }
      : undefined;
  const statuses = tanks.map((tank) => tankPowerState(data.rollups.get(asEntityKey(tank.id))?.miningStatus));
  const allOn = statuses.length > 0 && statuses.every((status) => status.on);
  return (
    <section aria-labelledby={`${side.toLowerCase()}-tanks-title`}>
      <dl className="mb-6 grid grid-cols-2 gap-6 tablet:grid-cols-4">
        <Stat label="Flow" value={formatFlowRate(flow, flowUnit)} />
        <Stat
          label={`${side} tank inlet temperature`}
          value={formatTemperature(latest(plant?.inletTemperature), unit)}
        />
        <Stat label={`${side} tank outlet temperature`} value={formatTemperature(latest(outlet), unit)} />
        <Stat label="Fluid level" value={liquidLevel === undefined ? "—" : `${decimal(liquidLevel)} mm`} />
      </dl>
      <Panel className="mb-1 flex items-center justify-between">
        <h3 id={`${side.toLowerCase()}-tanks-title`} className="text-emphasis-300">
          {side} tanks
        </h3>
        <Switch
          label="All on"
          ariaLabel={`${side} tanks power`}
          checked={allOn}
          disabled={
            !scope ||
            statuses.some((status) => !status.actionable) ||
            tanks.some((tank) => tank.capabilities.startStop !== "supported") ||
            !powerControl.supported ||
            powerControl.pending
          }
          setChecked={() => void powerControl.setPower(scope, !allOn)}
        />
      </Panel>
      <div className="grid gap-1 laptop:grid-cols-3">
        {tanks.map((tank) => (
          <TankDiagnosticCard key={tank.id} tank={tank} topology={data.topology} powerControl={powerControl} />
        ))}
      </div>
    </section>
  );
};

const TanksDiagnostics = ({ data }: { data: NonNullable<ReturnType<typeof useContainerDiagnostics>> }) => {
  const powerControl = useTankPower();
  const unassigned = data.tanks.filter((tank) => tankSide(tank.slot) === undefined);
  return (
    <section className="space-y-10" aria-labelledby="container-diagnostics-tanks">
      <h2 id="container-diagnostics-tanks" className="text-heading-200">
        Tanks
      </h2>
      {powerControl.error ? (
        <p role="alert" className="text-intent-critical-fill">
          {powerControl.error}
        </p>
      ) : null}
      {(["Left", "Right"] as const).map((side) => (
        <TankBank
          key={side}
          side={side}
          tanks={data.tanks.filter((tank) => tankSide(tank.slot) === side)}
          data={data}
          powerControl={powerControl}
        />
      ))}
      {unassigned.length > 0 ? (
        <section aria-label="Other tanks" className="grid gap-1 laptop:grid-cols-3">
          {unassigned.map((tank) => (
            <TankDiagnosticCard key={tank.id} tank={tank} topology={data.topology} powerControl={powerControl} />
          ))}
        </section>
      ) : null}
    </section>
  );
};

const DryCoolerDiagnostics = ({ data }: { data: NonNullable<ReturnType<typeof useContainerDiagnostics>> }) => {
  const { adapter } = useMinerHosting();
  const connectionGeneration = useMinerStore((state) => state.connectionGeneration);
  const [showSettings, setShowSettings] = useState(false);
  const [selectedFan, setSelectedFan] = useState<string>();
  const infoFan = data.fans.find((fan) => fan.id === selectedFan);
  const plant = data.plant;
  const pumpSpeed = latest(plant?.coolantPumpSpeed);
  const auto = plant?.controls?.find((control) => control.name === "dry_cooler_auto");
  const target = plant?.setpoints?.find((setpoint) => setpoint.name === "dry_cooler_auto_temp_c");
  const mode = auto?.stateKnown && typeof auto.enabled === "boolean" ? (auto.enabled ? "auto" : "manual") : undefined;
  const canManage = Boolean(adapter?.setContainerControl && adapter.setContainerSetpoint && mode && target);
  const refreshTelemetry = async () => {
    if (!adapter || useMinerStore.getState().connectionGeneration !== connectionGeneration) return;
    const accept = beginStoreWrite("telemetry:current:miner");
    const telemetry = await adapter.getCurrentTelemetry(["miner"]);
    if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
  };
  return (
    <section aria-labelledby="dry-cooler-title">
      <h2 id="dry-cooler-title" className="mb-6 text-heading-200">
        Cooling
      </h2>
      <dl className="mb-6 grid grid-cols-2 gap-6 laptop:grid-cols-5">
        <Stat label="Pump speed" value={pumpSpeed === undefined ? "—" : `${decimal(pumpSpeed)} Hz`} />
        <Stat label="Pump inlet pressure" value={formatPressure(latest(plant?.pumpInletPressure))} />
        <Stat label="Pump outlet pressure" value={formatPressure(latest(plant?.pumpOutletPressure))} />
        <Stat label="Loop inlet pressure" value={formatPressure(latest(plant?.heatRejectionInletPressure))} />
        <Stat label="Loop outlet pressure" value={formatPressure(latest(plant?.heatRejectionOutletPressure))} />
      </dl>
      <Panel className="mb-1 flex items-center justify-between">
        <div>
          <div className="text-emphasis-300">Mode</div>
          <div className="text-300 text-text-primary-70">
            {mode !== "auto" || target?.value === undefined ? "—" : `${decimal(target.value)}°C target inlet temp`}
          </div>
        </div>
        <div className="flex items-center gap-6">
          <span className="text-300 text-text-primary-70">
            {mode === "auto" ? "Auto" : mode === "manual" ? "Manual" : "—"}
          </span>
          <Button
            text="Manage"
            variant={variants.secondary}
            disabled={!canManage}
            ariaHasPopup="dialog"
            ariaExpanded={showSettings}
            onClick={() => setShowSettings(true)}
          />
        </div>
      </Panel>
      <div className="grid gap-1 tablet:grid-cols-2 desktop:grid-cols-5">
        {data.fans.map((fan) => {
          const speed = latest(fan.telemetry?.speedPercent);
          const faulted = fan.telemetry?.health === "fault" || fan.telemetry?.health === "degraded";
          return (
            <Panel key={fan.id} className="min-h-44">
              <div className="mb-2 flex items-center justify-between">
                <h3 className="flex items-center gap-2 text-emphasis-300">
                  {faulted ? <Alert className="text-intent-critical-fill" /> : null}
                  {fan.name}
                </h3>
                <InfoButton label={`${fan.name} info`} onClick={() => setSelectedFan(fan.id)} />
              </div>
              <p className="text-300 text-text-primary-70 tabular-nums">— RPM</p>
              <p className="text-300 text-text-primary-70 tabular-nums">
                {speed === undefined ? "—" : `${decimal(speed, 0)}% PWM`}
              </p>
              <div
                className="mt-4 text-center"
                role="img"
                aria-label={`${fan.name} PWM: ${speed === undefined ? "unavailable" : `${decimal(speed, 0)}%`}`}
              >
                {speed === undefined ? (
                  <span className="inline-flex h-14 w-14 items-center justify-center text-text-primary-70">—</span>
                ) : (
                  <RadialGauge
                    value={speed}
                    size={56}
                    strokeWidth={7}
                    valueStrokeWidth={7}
                    strokeLinecap="butt"
                    colorClassName={faulted ? "text-intent-critical-fill" : "text-text-primary"}
                  />
                )}
              </div>
            </Panel>
          );
        })}
      </div>
      {infoFan ? (
        <DiagnosticStatusModal
          title={`${infoFan.name} status`}
          onDismiss={() => setSelectedFan(undefined)}
          componentType="fan"
          summary="Fan diagnostics"
          errors={[]}
          metrics={[
            { label: "Speed", value: "— RPM" },
            {
              label: "PWM",
              value:
                latest(infoFan.telemetry?.speedPercent) === undefined
                  ? "—"
                  : `${decimal(latest(infoFan.telemetry?.speedPercent), 0)}%`,
            },
          ]}
          metadata={{ status: { label: "Health", value: healthLabels[infoFan.telemetry?.health ?? "unknown"] } }}
        />
      ) : null}
      {showSettings && canManage && mode && adapter ? (
        <DryCoolerSettingsModal
          adapter={adapter}
          initialMode={mode}
          initialTargetC={target?.value}
          onSaved={refreshTelemetry}
          onDismiss={() => setShowSettings(false)}
        />
      ) : null}
    </section>
  );
};

const distributionStatus = (telemetry: PlantTelemetry | undefined) => {
  if (telemetry?.health === "fault") return "Fault";
  if (telemetry?.powered === false || telemetry?.health === "offline" || telemetry?.activeFeedCount === 0) {
    return "Offline";
  }
  if (telemetry?.powered === true || (telemetry?.activeFeedCount ?? 0) > 0) return "Online";
  return "—";
};

const PowerDiagnostics = ({ data }: { data: NonNullable<ReturnType<typeof useContainerDiagnostics>> }) => (
  <section aria-labelledby="power-diagnostics-title">
    <h2 id="power-diagnostics-title" className="mb-6 text-heading-200">
      Power
    </h2>
    <Panel>
      <dl className="grid grid-cols-2 gap-8 tablet:grid-cols-4 desktop:grid-cols-7">
        <Stat
          label="Container"
          value={data.plant?.powered === undefined ? "—" : data.plant.powered ? "Online" : "Offline"}
        />
        {data.powerComponents.map(({ hardware, telemetry }) => (
          <Stat
            key={telemetry?.componentId ?? hardware.name}
            label={hardware.name}
            value={distributionStatus(telemetry)}
          />
        ))}
      </dl>
    </Panel>
  </section>
);

const controlLabels = [
  ["air_conditioner_1", "AC 1"],
  ["air_conditioner_2", "AC 2"],
  ["coolant_pump", "Coolant pump"],
  ["alarm", "Alarm"],
  ["tank_led_light", "Tank lights"],
] as const;

const ControlsDiagnostics = ({ plant }: { plant?: PlantTelemetry }) => {
  const { setControl, pending, error, supported } = useContainerControl();
  const pumpSpeed = latest(plant?.coolantPumpSpeed);
  const flow = latest(plant?.flowRate);
  const flowUnit = useFlowUnit();
  return (
    <section aria-labelledby="controls-diagnostics-title">
      <h2 id="controls-diagnostics-title" className="mb-6 text-heading-200">
        Controls
      </h2>
      {error ? (
        <p role="alert" className="mb-4 text-intent-critical-fill">
          {error}
        </p>
      ) : null}
      <div className="grid gap-1 tablet:grid-cols-2">
        {controlLabels.map(([name, label]) => {
          const control = plant?.controls?.find((item) => item.name === name);
          const isAlarm = name === "alarm";
          let status = "—";
          if (isAlarm) {
            if (plant?.alarmActive !== undefined) status = plant.alarmActive ? "Active" : "Inactive";
          } else if (control?.stateKnown) {
            status = control.enabled ? "On" : "Off";
          }
          return (
            <Panel key={name} className="flex items-center justify-between gap-5">
              <span className="text-emphasis-300">{label}</span>
              <div className="flex items-center gap-5">
                <span className="text-300 text-text-primary-70">
                  {name === "coolant_pump"
                    ? `${decimal(pumpSpeed)} Hz input, ${formatFlowRate(flow, flowUnit)}`
                    : status}
                </span>
                {!isAlarm ? (
                  <Switch
                    ariaLabel={label}
                    checked={Boolean(control?.enabled)}
                    disabled={!supported || !control?.stateKnown || pending}
                    setChecked={() => void setControl(name, !control?.enabled)}
                  />
                ) : null}
              </div>
            </Panel>
          );
        })}
      </div>
    </section>
  );
};

const ContainerDiagnosticView = ({ className }: { className?: string }) => {
  const [selectedTab, setSelectedTab] = useState<DiagnosticTab>("all");
  const data = useContainerDiagnostics();
  const full = selectedTab === "all" || selectedTab === "tanks";
  const polling = <DiagnosticsPolling key={full ? "full" : "plant"} full={full} />;
  if (!data)
    return (
      <>
        {polling}
        <p role="status">Loading container systems…</p>
      </>
    );
  const show = (tab: Exclude<DiagnosticTab, "all">) => selectedTab === "all" || selectedTab === tab;
  return (
    <div className={clsx("w-full", className)} data-testid="container-diagnostics">
      {polling}
      <div className="mb-10 flex flex-col items-start justify-between gap-4 tablet:flex-row tablet:items-center">
        <h1 className="text-heading-300">Systems</h1>
        <div className="max-w-full overflow-x-auto">
          <SegmentedControl
            segments={tabs}
            initialSegmentKey="all"
            onSelect={(key) => setSelectedTab(key as DiagnosticTab)}
          />
        </div>
      </div>
      <div className="space-y-12">
        {selectedTab === "all" ? <ContainerSummary plant={data.plant} rollup={data.containerRollup} /> : null}
        {show("tanks") ? <TanksDiagnostics data={data} /> : null}
        {show("dry-cooler") ? <DryCoolerDiagnostics data={data} /> : null}
        {show("power") ? <PowerDiagnostics data={data} /> : null}
        {show("controls") ? <ControlsDiagnostics plant={data.plant} /> : null}
      </div>
    </div>
  );
};

export default ContainerDiagnosticView;
