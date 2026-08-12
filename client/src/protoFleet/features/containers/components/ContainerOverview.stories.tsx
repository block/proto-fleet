import { useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import type { ContainerToggleControl } from "./ContainerControls";
import ContainerOverview, {
  type ContainerFan,
  type ContainerOverviewProps,
  type ContainerTank,
} from "./ContainerOverview";
import type { TankModuleState } from "./TankModuleGrid";
import {
  AggregatedValueSchema,
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

/** Deterministic shuffle so the module layout is stable across renders. */
function seededShuffle<T>(arr: T[], seed: number): T[] {
  const result = [...arr];
  let s = seed;
  for (let i = result.length - 1; i > 0; i--) {
    s = (s * 16807) % 2147483647;
    const j = s % (i + 1);
    [result[i], result[j]] = [result[j], result[i]];
  }
  return result;
}

/** Build a shuffled module array with `attention` needing-attention bars. */
function makeModules(total: number, attention = 0, seed = 7): TankModuleState[] {
  const modules: TankModuleState[] = [];
  for (let i = 0; i < Math.min(attention, total); i++) modules.push("attention");
  while (modules.length < total) modules.push("healthy");
  return seededShuffle(modules, seed);
}

// A 16-module tank renders as 8 columns × 2 rows, matching the design's two-row module grid.
const TANK_COLS = 8;
const TANK_ROWS = 2;
const MODULES_PER_TANK = TANK_COLS * TANK_ROWS;

const initialTanks: ContainerTank[] = [
  {
    id: "t1",
    label: "Tank 1",
    on: true,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 0, 11),
    stats: ["48/48 boards", "65.5°", "12.3 kW"],
  },
  {
    id: "t2",
    label: "Tank 2",
    on: true,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 2, 23),
    stats: ["44/48 boards", "67.1°", "11.8 kW"],
  },
  {
    id: "t3",
    label: "Tank 3",
    on: true,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 1, 37),
    stats: ["45/48 boards", "64.2°", "11.5 kW"],
  },
  {
    id: "t4",
    label: "Tank 4",
    on: true,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 3, 41),
    stats: ["42/48 boards", "68.9°", "11.1 kW"],
  },
  {
    id: "t5",
    label: "Tank 5",
    on: true,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 0, 53),
    stats: ["48/48 boards", "65.0°", "12.4 kW"],
  },
  {
    id: "t6",
    label: "Tank 6",
    on: false,
    cols: TANK_COLS,
    rows: TANK_ROWS,
    modules: makeModules(MODULES_PER_TANK, 0, 67),
    stats: ["0/48 boards", "—", "0.0 kW"],
  },
];

const initialFans: ContainerFan[] = [
  { id: "f1", label: "Fan 1", on: true, speedPercent: 68, speedLabel: "3,200" },
  { id: "f2", label: "Fan 2", on: true, speedPercent: 74, speedLabel: "3,480" },
  { id: "f3", label: "Fan 3", on: true, speedPercent: 61, speedLabel: "2,870" },
  { id: "f4", label: "Fan 4", on: false, speedPercent: 0, speedLabel: "0" },
  { id: "f5", label: "Fan 5", on: true, speedPercent: 71, speedLabel: "3,340" },
  { id: "f6", label: "Fan 6", on: true, speedPercent: 66, speedLabel: "3,100" },
  { id: "f7", label: "Fan 7", on: true, speedPercent: 79, speedLabel: "3,710" },
  { id: "f8", label: "Fan 8", on: true, speedPercent: 58, speedLabel: "2,720" },
  { id: "f9", label: "Fan 9", on: true, speedPercent: 72, speedLabel: "3,380" },
  { id: "f10", label: "Fan 10", on: true, speedPercent: 64, speedLabel: "3,010" },
];

const initialControls: ContainerToggleControl[] = [
  { id: "ac-1", label: "AC 1", metric: "75°F", icon: "fan", on: true },
  { id: "ac-2", label: "AC 2", metric: "75°F", icon: "fan", on: true },
  { id: "cdu-fans", label: "CDU cooling fans", metric: "60% speed", icon: "fan", on: true },
  { id: "coolant-pump", label: "Coolant pump", metric: "50 Hz", icon: "pump", on: true },
  { id: "dry-cooler", label: "Dry cooler", metric: "118°F, 50% speed", icon: "thermometer", on: true },
  { id: "dry-cooler-auto", label: "Dry cooler auto", icon: "thermometer", on: true },
  { id: "tank-a-light", label: "Tank A light", icon: "light", on: true },
  { id: "tank-b-light", label: "Tank B light", icon: "light", on: true },
  { id: "logo-light", label: "Logo light", icon: "light", on: true },
];

const breadcrumb = [{ label: "Sites", to: "/fleet/sites" }, { label: "Kati 1A", to: "/sites/1" }, { label: "CT1-01" }];

/**
 * Deterministic mock telemetry so the performance charts render with a stable,
 * realistic curve in Storybook. Mirrors the shape the rack/building detail
 * views feed DeviceSetPerformanceSection: one metric per measurement type per
 * hourly bucket over the last 24h, each carrying avg/min/max aggregates (the
 * temperature headline formats a min–max range, so all three are needed).
 */
function makeMetrics(): Metric[] {
  const now = Math.floor(Date.now() / 1000);
  const hour = 3600;
  const points = 24;
  const series: { type: MeasurementType; base: number; amp: number; spread: number }[] = [
    { type: MeasurementType.HASHRATE, base: 288_400, amp: 8_000, spread: 6_000 },
    { type: MeasurementType.TEMPERATURE, base: 65.5, amp: 2.5, spread: 3.5 },
    { type: MeasurementType.EFFICIENCY, base: 3_649.8, amp: 40, spread: 30 },
    { type: MeasurementType.POWER, base: 1_000, amp: 25, spread: 20 },
  ];
  const metrics: Metric[] = [];
  for (const { type, base, amp, spread } of series) {
    for (let i = 0; i < points; i++) {
      const avg = base + Math.sin(i / 3) * amp;
      metrics.push(
        create(MetricSchema, {
          measurementType: type,
          openTime: { seconds: BigInt(now - (points - 1 - i) * hour), nanos: 0 },
          deviceCount: 1,
          aggregatedValues: [
            create(AggregatedValueSchema, { aggregationType: AggregationType.AVERAGE, value: avg }),
            create(AggregatedValueSchema, { aggregationType: AggregationType.MIN, value: avg - spread }),
            create(AggregatedValueSchema, { aggregationType: AggregationType.MAX, value: avg + spread }),
          ],
        }),
      );
    }
  }
  return metrics;
}

const metrics = makeMetrics();

const kpis: ContainerOverviewProps["kpis"] = [
  { label: "Hashrate", value: "288.4", units: "PH/s" },
  { label: "Power", value: "1.0", units: "MW" },
  { label: "Efficiency", value: "3,649.8", units: "J/TH" },
  { label: "Modules online", value: "94/96" },
];

/** Interactive wrapper: owns tank, fan, and auxiliary control state so every toggle flips live in the story. */
function InteractiveContainerOverview() {
  const [tanks, setTanks] = useState(initialTanks);
  const [fans, setFans] = useState(initialFans);
  const [controls, setControls] = useState(initialControls);

  const props: ContainerOverviewProps = useMemo(
    () => ({
      breadcrumb,
      title: "CT1-01",
      kpis,
      tanks,
      fans,
      controls,
      metrics,
      onToggleTank: (id, on) => setTanks((prev) => prev.map((t) => (t.id === id ? { ...t, on } : t))),
      onToggleFan: (id, on) => setFans((prev) => prev.map((f) => (f.id === id ? { ...f, on } : f))),
      onToggleControl: (id, on) =>
        setControls((prev) => prev.map((control) => (control.id === id ? { ...control, on } : control))),
      onResetAlarm: () => {},
      onMuteAlarm: () => {},
      onTankInfo: () => {},
      onFanInfo: () => {},
      onSelectTank: () => {},
      onViewMiners: () => {},
      onViewDetails: () => {},
    }),
    [tanks, fans, controls],
  );

  return <ContainerOverview {...props} />;
}

const meta: Meta<typeof InteractiveContainerOverview> = {
  title: "Proto Fleet/Containers/Container Overview",
  component: InteractiveContainerOverview,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Container overview / tank heatmap (Frame 1): breadcrumb + KPI header, elevated panels for tank cards, the 5×2 fan grid, and container-only auxiliary Controls (AC, CDU fans, coolant pump, dry cooler, lights, and alarm actions), followed by the shared rack/building Performance section. Every prototype control is caller-owned local state; the components do not inspect device identity or call a backend.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof InteractiveContainerOverview>;

export const Default: Story = {};
