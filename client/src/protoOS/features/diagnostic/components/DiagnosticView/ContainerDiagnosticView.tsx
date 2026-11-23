import { type ReactNode, useId, useMemo, useState } from "react";
import clsx from "clsx";

import ControllerDiagnostics from "./ControllerDiagnostics";
import { observationState } from "./controllerObservation";
import { Panel, Stat } from "./diagnosticLayout";
import DiagnosticStatusModal from "./DiagnosticStatusModal";
import DryCoolerSettingsModal from "./DryCoolerSettingsModal";
import { useContainerControl } from "./useContainerControl";
import { useControllerDiagnosticsSupported } from "./useControllerDiagnostics";
import { usePowerFeeds } from "./usePowerFeeds";
import { mappingRunActive } from "@/protoOS/api/adapters/feedMapping";
import type { MiningScope, TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useScopeHealth } from "@/protoOS/components/StatusModal/useScopeHealth";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { PLANT_OBSERVATION_STALE_MS } from "@/protoOS/features/containerHome/utils/plantAlarms";
import { useFeedMappingState } from "@/protoOS/features/feedMapping/feedMappingContext";
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
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import {
  asEntityKey,
  type ContainerTopology,
  type FanTelemetry,
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
import { Alert, ChevronDown, Info } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import RadialGauge from "@/shared/components/RadialGauge";
import SegmentedControl from "@/shared/components/SegmentedControl";
import StatusCircle from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";
import Switch from "@/shared/components/Switch";
import type { TemperatureUnit } from "@/shared/features/preferences/types";
import { formatFlowRate } from "@/shared/utils/flowRate";
import { convertCtoF } from "@/shared/utils/telemetryFormat";

type DiagnosticTab = "all" | "tanks" | "dry-cooler" | "power" | "controls" | "controllers";

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
  { key: "controllers", title: "Controllers" },
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

type PowerFeed = {
  id: string;
  enabled: boolean | undefined;
  faulted: boolean;
  /** The module this outlet powers, such as T2-M07, when the controller map names it. */
  module?: string;
};

const feedEnabled = (state: string | undefined) =>
  state === "POWER_FEED_STATE_ENABLED" ? true : state === "POWER_FEED_STATE_DISABLED" ? false : undefined;

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
    // Boards report the outlet that feeds them, which names the module on each outlet.
    const feedModules = new Map<string, string>();
    for (const board of topology.hashboards.values()) {
      const module = topology.modules.get(board.moduleId);
      const tank = module?.tankId ? topology.tanks.get(module.tankId) : undefined;
      const slot = module?.location?.moduleSlot;
      if (board.powerFeedId && tank?.slot !== undefined && slot !== undefined) {
        feedModules.set(board.powerFeedId, `T${tank.slot}-M${String(slot).padStart(2, "0")}`);
      }
    }
    const powerFeeds = [...plantHardware.entries()]
      .filter(([, hardware]) => hardware.kind === "powerFeed")
      .map(([id, hardware]): PowerFeed => {
        const telemetry = plantTelemetry.get(id);
        return {
          id: hardware.name,
          enabled: feedEnabled(telemetry?.operatingState),
          faulted: telemetry?.health === "fault",
          module: feedModules.get(hardware.name),
        };
      });
    // The controller does not attribute outlets to a PDU; CT1 names them
    // "<pdu>-outlet-NN", so the PDU's reported ID is the outlet prefix.
    const powerComponents = [...plantHardware.entries()]
      .filter(([, hardware]) => hardware.kind === "distribution")
      .map(([id, hardware]) => ({
        hardware,
        telemetry: plantTelemetry.get(id),
        feeds: powerFeeds.filter((feed) => feed.id.startsWith(`${hardware.name}-`)),
      }));
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
  const pumpPressureDelta =
    latest(plant?.pumpPressureDelta) ??
    (pumpInlet === undefined || pumpOutlet === undefined ? undefined : pumpOutlet - pumpInlet);
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
          <Stat label="Ambient temperature" value={formatTemperature(latest(plant?.outsideAmbientTemperature), unit)} />
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
          <Stat label="Pressure delta" value={formatPressure(pumpPressureDelta)} />
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

type PowerConfirmation = { scope: MiningScope; on: boolean; label: string; generation: number };
type TankPower = ReturnType<typeof useTankPower> & {
  requestPower: (scope: MiningScope | undefined, on: boolean, label: string) => void;
};

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
    <article className="rounded-2xl bg-surface-overlay p-6" aria-label={tank.label ?? `Tank ${tank.slot ?? ""}`}>
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
            setChecked={() => powerControl.requestPower(scope, !on, tank.label ?? "Tank")}
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
          setChecked={() => powerControl.requestPower(scope, !allOn, `${side} tanks`)}
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
  const power = useTankPower();
  const [confirmation, setConfirmation] = useState<PowerConfirmation>();
  const generation = useMinerStore((state) => state.connectionGeneration);
  const powerControl: TankPower = {
    ...power,
    requestPower: (scope, on, label) => {
      if (scope) setConfirmation({ scope, on, label, generation });
    },
  };
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
      {confirmation && confirmation.generation === generation ? (
        <Dialog
          title={`${confirmation.on ? "Turn on" : "Turn off"} ${confirmation.label}?`}
          subtitle={`This will ${confirmation.on ? "start" : "stop"} mining on ${confirmation.scope.modules?.length} modules in ${confirmation.label}.`}
          manageFocus
          preventScroll
          onDismiss={() => setConfirmation(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirmation(undefined) },
            {
              text: confirmation.on ? "Turn on" : "Turn off",
              variant: variants.primary,
              disabled: power.pending,
              onClick: () => {
                setConfirmation(undefined);
                if (useMinerStore.getState().connectionGeneration === confirmation.generation) {
                  void power.setPower(confirmation.scope, confirmation.on);
                }
              },
            },
          ]}
        />
      ) : null}
    </section>
  );
};

// Fan PWM and power share the plant snapshot, so they go stale together on its window.
const isFanStale = (telemetry: FanTelemetry | undefined, nowMs: number) =>
  observationState({ freshness: "fresh", observedAtMs: telemetry?.observedAtMs }, nowMs, PLANT_OBSERVATION_STALE_MS) ===
  "stale";

// Measured fan draw only; PWM is never converted into an estimate.
const FanPower = ({ telemetry, stale }: { telemetry: FanTelemetry | undefined; stale: boolean }) => {
  if (stale) return <span aria-label="Power stale">— kW · Stale</span>;
  const watts = latest(telemetry?.power);
  if (watts === undefined) return <span aria-label="Power unavailable">— kW</span>;
  // Below 1 kW the kW scale would round a slow fan's draw to a misleading "0.0 kW". Compare the
  // rounded value so 999.5 W reads "1.0 kW" rather than "1,000 W".
  return <span>{Math.round(Math.abs(watts)) < 1_000 ? `${decimal(watts, 0)} W` : `${decimal(watts / 1_000)} kW`}</span>;
};

const DryCoolerDiagnostics = ({ data }: { data: NonNullable<ReturnType<typeof useContainerDiagnostics>> }) => {
  const { adapter } = useMinerHosting();
  const connectionGeneration = useMinerStore((state) => state.connectionGeneration);
  const [showSettings, setShowSettings] = useState(false);
  const [selectedFan, setSelectedFan] = useState<string>();
  const infoFan = data.fans.find((fan) => fan.id === selectedFan);
  const nowMs = usePsuObservationTime(data.fans.some((fan) => fan.telemetry?.observedAtMs !== undefined));
  const infoStale = isFanStale(infoFan?.telemetry, nowMs);
  const infoSpeed = infoStale ? undefined : latest(infoFan?.telemetry?.speedPercent);
  const plant = data.plant;
  const pumpSpeed = latest(plant?.coolantPumpSpeed);
  const auto = plant?.controls?.find((control) => control.name === "dry_cooler_auto");
  const target = plant?.setpoints?.find((setpoint) => setpoint.name === "dry_cooler_auto_temp_c");
  const fanSpeed = plant?.setpoints?.find((setpoint) => setpoint.name === "dry_cooler_manual_speed_percent");
  const mode = auto?.stateKnown && typeof auto.enabled === "boolean" ? (auto.enabled ? "auto" : "manual") : undefined;
  const canManage = Boolean(
    adapter?.setContainerControl && adapter.setContainerSetpoint && mode && (target || fanSpeed),
  );
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
          const stale = isFanStale(fan.telemetry, nowMs);
          const speed = stale ? undefined : latest(fan.telemetry?.speedPercent);
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
              <p className="text-300 text-text-primary-70 tabular-nums">
                {speed === undefined ? "—" : `${decimal(speed, 0)}% PWM`}
              </p>
              <p className="text-300 text-text-primary-70 tabular-nums">
                <FanPower telemetry={fan.telemetry} stale={stale} />
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
            { label: "PWM", value: infoSpeed === undefined ? "—" : `${decimal(infoSpeed, 0)}%` },
            { label: "Power", value: <FanPower telemetry={infoFan.telemetry} stale={infoStale} /> },
          ]}
          metadata={{ status: { label: "Health", value: healthLabels[infoFan.telemetry?.health ?? "unknown"] } }}
        />
      ) : null}
      {showSettings && canManage && mode && adapter ? (
        <DryCoolerSettingsModal
          adapter={adapter}
          initialMode={mode}
          initialTargetC={target?.value}
          initialFanSpeedPercent={fanSpeed?.value}
          onSaved={refreshTelemetry}
          onDismiss={() => setShowSettings(false)}
        />
      ) : null}
    </section>
  );
};

type PowerStatus = "Online" | "Offline" | "Fault" | "Unknown";

const distributionStatus = (telemetry: PlantTelemetry | undefined): PowerStatus => {
  if (telemetry?.health === "fault") return "Fault";
  if (telemetry?.powered === false || telemetry?.health === "offline" || telemetry?.activeFeedCount === 0) {
    return "Offline";
  }
  if (telemetry?.powered === true || (telemetry?.activeFeedCount ?? 0) > 0) return "Online";
  return "Unknown";
};

const powerStatusColors: Record<PowerStatus, StatusCircleStatus> = {
  Online: "normal",
  Offline: "inactive",
  Fault: "error",
  Unknown: "pending",
};

// One full-width row per power source: a status dot, the name, a short summary,
// and the switch that controls it. A row with outlets leads with the control
// that opens them directly below, as the expandable rows in Fleet do.
const PowerSource = ({
  name,
  status,
  note,
  expand,
  outlets,
  children,
}: {
  name: string;
  status: PowerStatus;
  note?: string;
  /** Shows a control that opens this source's outlets in the region with this id. */
  expand?: { open: boolean; controls: string; onToggle: () => void };
  /** The open outlet list, shown under the row. */
  outlets?: ReactNode;
  children: ReactNode;
}) => (
  <li className="border-t border-border-10 first:border-t-0">
    <div className="flex min-h-12 items-center gap-2">
      {expand ? (
        <button
          type="button"
          aria-label={`${name} outlets`}
          aria-expanded={expand.open}
          aria-controls={expand.controls}
          onClick={expand.onToggle}
          className="-ml-2 flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-sm focus-visible:outline-2 focus-visible:outline-core-accent-fill"
        >
          <ChevronDown
            width="w-3"
            className={clsx("text-text-primary-70 transition-transform", !expand.open && "-rotate-90")}
          />
        </button>
      ) : (
        <span className="-ml-2 size-8 shrink-0" />
      )}
      <span role="img" aria-label={`${name} status: ${status}`} title={status} className="flex shrink-0">
        <StatusCircle status={powerStatusColors[status]} variant="simple" width="w-2" removeMargin />
      </span>
      <span className="flex min-w-0 flex-1 items-baseline gap-3">
        <span className="truncate text-emphasis-300">{name}</span>
        {note ? <span className="shrink-0 text-200 text-text-primary-70">{note}</span> : null}
      </span>
      <span className="flex shrink-0 items-center">{children}</span>
    </div>
    {outlets}
  </li>
);

const outletName = (feed: PowerFeed, pdu: string) => {
  const rest = feed.id.slice(pdu.length + 1);
  const number = rest.match(/^outlet-(\d+)$/)?.[1];
  return number ? `Outlet ${number}` : rest;
};

const outletStatus = (feed: PowerFeed): PowerStatus =>
  feed.faulted ? "Fault" : feed.enabled === undefined ? "Unknown" : feed.enabled ? "Online" : "Offline";

const statusNote = (status: PowerStatus) => (status === "Online" ? undefined : status);

// A PDU that is not online says so; an online PDU with some outlets off says how many.
const pduNote = (status: PowerStatus, feeds: PowerFeed[]) => {
  const off = feeds.filter((feed) => feed.enabled === false).length;
  return statusNote(status) ?? (off > 0 && off < feeds.length ? `${off} of ${feeds.length} off` : undefined);
};

const feedPowerState = (feeds: PowerFeed[]) => ({
  on: feeds.length > 0 && feeds.every((feed) => feed.enabled === true),
  known: feeds.length > 0 && feeds.every((feed) => feed.enabled !== undefined),
});

// A switch change waiting for confirmation: the direction the switch moves to,
// and the outlets that are not in that state yet.
type FeedConfirmation = {
  on: boolean;
  ids: string[];
  label: string;
  generation: number;
  /** Set for one outlet: the module it powers, or undefined when the map does not name it. */
  outlet?: { module?: string };
};

const PowerDiagnostics = ({ data }: { data: NonNullable<ReturnType<typeof useContainerDiagnostics>> }) => {
  const power = usePowerFeeds();
  const [confirmation, setConfirmation] = useState<FeedConfirmation>();
  const generation = useMinerStore((state) => state.connectionGeneration);
  // An outlet mapping run switches outlets itself, so the operator's switches wait for it.
  const mappingActive = mappingRunActive(useFeedMappingState()?.run);
  const allFeeds = data.powerComponents.flatMap((component) => component.feeds);
  const all = feedPowerState(allFeeds);
  // A switch reads on only when all its outlets are on, so it moves to off only
  // from that state. From off or partly off it moves to on. Outlets already in
  // the target state are left alone.
  const requestPower = (feeds: PowerFeed[], label: string, outlet?: FeedConfirmation["outlet"]) => {
    const on = !feedPowerState(feeds).on;
    const ids = feeds.filter((feed) => feed.enabled === !on).map((feed) => feed.id);
    if (ids.length) setConfirmation({ on, ids, label, generation, outlet });
  };
  // One PDU's outlets are open at a time, directly under that PDU.
  const [openPdu, setOpenPdu] = useState<string>();
  const outletsId = useId();
  const outletTarget = confirmation?.outlet
    ? confirmation.outlet.module
      ? `module ${confirmation.outlet.module}`
      : "the module on this outlet"
    : undefined;
  const outlets = (count: number) => `${count} ${count === 1 ? "outlet" : "outlets"}`;
  const containerStatus: PowerStatus =
    data.plant?.powered === undefined ? "Unknown" : data.plant.powered ? "Online" : "Offline";
  const locked = !power.supported || power.pending || mappingActive;
  const confirm = () => {
    if (!confirmation) return;
    setConfirmation(undefined);
    if (useMinerStore.getState().connectionGeneration === confirmation.generation) {
      void power.setFeeds(confirmation.ids, confirmation.on);
    }
  };
  return (
    <section aria-labelledby="power-diagnostics-title">
      <h2 id="power-diagnostics-title" className="mb-6 text-heading-200">
        Power
      </h2>
      {power.error ? (
        <p role="alert" className="mb-4 text-intent-critical-fill">
          {power.error}
        </p>
      ) : null}
      {mappingActive ? (
        <p className="mb-4 text-300 text-text-primary-70">
          Outlet mapping is running. PDU switches are locked until it finishes.
        </p>
      ) : null}
      <Panel>
        <ul>
          <PowerSource
            name="Container"
            status={containerStatus}
            note={statusNote(containerStatus) ?? (data.powerComponents.length > 0 ? "All PDUs" : undefined)}
          >
            {data.powerComponents.length > 0 ? (
              <Switch
                ariaLabel="All PDUs power"
                checked={all.on}
                disabled={locked || !all.known}
                setChecked={() => requestPower(allFeeds, "all PDUs")}
              />
            ) : null}
          </PowerSource>
          {data.powerComponents.map(({ hardware, telemetry, feeds }) => {
            const state = feedPowerState(feeds);
            const status = distributionStatus(telemetry);
            const open = openPdu === hardware.name && feeds.length > 0;
            const regionId = `${outletsId}-${hardware.name}`;
            return (
              <PowerSource
                key={telemetry?.componentId ?? hardware.name}
                name={hardware.name}
                status={status}
                note={pduNote(status, feeds) ?? (feeds.length > 0 ? outlets(feeds.length) : undefined)}
                expand={
                  feeds.length > 0
                    ? {
                        open,
                        controls: regionId,
                        onToggle: () => setOpenPdu(open ? undefined : hardware.name),
                      }
                    : undefined
                }
                outlets={
                  open ? (
                    <section id={regionId} aria-label={`${hardware.name} outlets`} className="pb-3 pl-6">
                      <ul>
                        {feeds.map((feed) => {
                          const outlet = outletStatus(feed);
                          const name = outletName(feed, hardware.name);
                          const exception = outlet === "Fault" || outlet === "Unknown" ? outlet : undefined;
                          return (
                            <PowerSource
                              key={feed.id}
                              name={feed.module ?? name}
                              status={outlet}
                              note={feed.module ? [name, exception].filter(Boolean).join(" · ") : exception}
                            >
                              <Switch
                                ariaLabel={`${feed.id} power`}
                                checked={feed.enabled === true}
                                disabled={locked || feed.enabled === undefined}
                                setChecked={() =>
                                  requestPower([feed], `${name} on ${hardware.name}`, { module: feed.module })
                                }
                              />
                            </PowerSource>
                          );
                        })}
                      </ul>
                    </section>
                  ) : undefined
                }
              >
                <Switch
                  ariaLabel={`${hardware.name} power`}
                  checked={state.on}
                  disabled={locked || !state.known}
                  setChecked={() => requestPower(feeds, hardware.name)}
                />
              </PowerSource>
            );
          })}
        </ul>
      </Panel>
      {confirmation && confirmation.generation === generation ? (
        <Dialog
          title={`${confirmation.on ? "Power on" : "Power off"} ${confirmation.label}?`}
          subtitle={
            outletTarget
              ? confirmation.on
                ? `This restores line power to ${outletTarget}. It will boot.`
                : `This cuts line power to ${outletTarget}. It stops hashing and stays off until the outlet is powered on again.`
              : confirmation.on
                ? `This restores line power to ${outlets(confirmation.ids.length)} on ${confirmation.label}. Modules on those outlets will boot.`
                : `This cuts line power to ${outlets(confirmation.ids.length)} on ${confirmation.label}. Modules on those outlets stop hashing and stay off until the outlets are powered on again.`
          }
          manageFocus
          preventScroll
          onDismiss={() => setConfirmation(undefined)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setConfirmation(undefined) },
            {
              text: confirmation.on ? "Power on" : "Power off",
              variant: confirmation.on ? variants.primary : variants.danger,
              disabled: locked,
              onClick: confirm,
            },
          ]}
        />
      ) : null}
    </section>
  );
};

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
  const controllersSupported = useControllerDiagnosticsSupported();
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
            segments={controllersSupported ? tabs : tabs.filter((tab) => tab.key !== "controllers")}
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
        {show("controllers") ? <ControllerDiagnostics /> : null}
      </div>
    </div>
  );
};

export default ContainerDiagnosticView;
