import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { FleetShellStoryDecorator } from "./FleetShellStoryDecorator";
import SubtankDetail, { type SubtankDetailProps } from "./SubtankDetail";
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

// A 16-module tank reads as 8 columns × 2 rows in the rack-detail grid, the same
// layout its overview tank card uses.
const TANK_COLS = 8;
const TANK_ROWS = 2;
const MODULES_PER_TANK = TANK_COLS * TANK_ROWS;

/**
 * Deterministic mock telemetry so the performance charts render with a stable,
 * realistic curve in Storybook. Mirrors the shape the rack/building detail views
 * feed DeviceSetPerformanceSection: one metric per measurement type per hourly
 * bucket over the last 24h, each carrying avg/min/max aggregates.
 */
function makeMetrics(): Metric[] {
  const now = Math.floor(Date.now() / 1000);
  const hour = 3600;
  const points = 24;
  const series: { type: MeasurementType; base: number; amp: number; spread: number }[] = [
    { type: MeasurementType.HASHRATE, base: 18_025, amp: 500, spread: 400 },
    { type: MeasurementType.TEMPERATURE, base: 67.1, amp: 2.5, spread: 3.5 },
    { type: MeasurementType.EFFICIENCY, base: 3_649.8, amp: 40, spread: 30 },
    { type: MeasurementType.POWER, base: 62.5, amp: 2, spread: 1.5 },
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

// Tank 2 on the overview — 14/16 modules healthy, two need attention. modules
// is the same row-major array the overview TankCard renders.
const tank2Modules = makeModules(MODULES_PER_TANK, 2, 23);

const baseProps: SubtankDetailProps = {
  breadcrumb: [
    { label: "Sites", to: "/fleet/sites" },
    { label: "Kati 1A", to: "/sites/1" },
    { label: "Container 01", to: "/containers/1" },
    { label: "Tank 2" },
  ],
  title: "Container 01 / Tank 2",
  subtitle: "Immersion",
  kpis: [
    { label: "Hashrate", value: "18.0", units: "PH/s" },
    { label: "Power", value: "62.5", units: "kW" },
    { label: "Efficiency", value: "3,649.8", units: "J/TH" },
    { label: "Modules online", value: "14/16" },
  ],
  rows: TANK_ROWS,
  cols: TANK_COLS,
  modules: tank2Modules,
  on: true,
  tankLabel: "Tank 2",
  metrics,
  onViewMiners: () => {},
  onModuleAction: () => {},
};

const meta: Meta<typeof SubtankDetail> = {
  title: "Proto Containers/Overview/Subtank Detail",
  component: SubtankDetail,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Subtank detail — a single container tank rendered as the tank analogue of RackOverviewPage: Breadcrumb + Header title bar, a KPI header, the TankHealthModule, and the shared DeviceSetPerformanceSection. Per jmarr (2026-08-10) the health section keeps the tank's own module language — the same 3-segment ModuleTile bars the overview card uses, scaled up and numbered 01..N with a status breakdown beside them — instead of switching to the Fleet numbered-slot grid, so the card and the detail read as one surface. Each bar carries the same View / Blink LEDs / Reboot / Sleep action menu the overview card exposes. Presentational and prop-driven (prototype-first) — a tank has no backend DeviceSet yet.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof SubtankDetail>;

export const Default: Story = {
  args: baseProps,
};

// A tank with no flagged modules — all modules healthy.
const tank1Modules = makeModules(MODULES_PER_TANK, 0, 11);

export const AllHealthy: Story = {
  args: {
    ...baseProps,
    breadcrumb: [
      { label: "Sites", to: "/fleet/sites" },
      { label: "Kati 1A", to: "/sites/1" },
      { label: "Container 01", to: "/containers/1" },
      { label: "Tank 1" },
    ],
    title: "Container 01 / Tank 1",
    tankLabel: "Tank 1",
    kpis: [
      { label: "Hashrate", value: "18.4", units: "PH/s" },
      { label: "Power", value: "64.0", units: "kW" },
      { label: "Efficiency", value: "3,478.3", units: "J/TH" },
      { label: "Modules online", value: "16/16" },
    ],
    modules: tank1Modules,
    on: true,
  },
};

// A powered-off tank — every populated module reads offline, mirroring a
// de-energized rack in the fleet view.
const tank6Modules = makeModules(MODULES_PER_TANK, 0, 67);

export const PoweredOff: Story = {
  args: {
    ...baseProps,
    breadcrumb: [
      { label: "Sites", to: "/fleet/sites" },
      { label: "Kati 1A", to: "/sites/1" },
      { label: "Container 01", to: "/containers/1" },
      { label: "Tank 6" },
    ],
    title: "Container 01 / Tank 6",
    tankLabel: "Tank 6",
    kpis: [
      { label: "Hashrate", value: "0.0", units: "PH/s" },
      { label: "Power", value: "0.0", units: "kW" },
      { label: "Efficiency", value: "—" },
      { label: "Modules online", value: "0/16" },
    ],
    modules: tank6Modules,
    on: false,
    metrics: [],
  },
};

// ----------------------------------------------------------------------------
// Subtank detail inside the REAL Fleet app chrome (icon nav rail), via the
// shared FleetShellStoryDecorator — the same shell + page composition the demo
// shows on :6060.
// ----------------------------------------------------------------------------
export const InFleetShell: Story = {
  name: "In Fleet Shell (icon nav)",
  parameters: {
    docs: {
      description: {
        story:
          "Subtank detail mounted inside the REAL Fleet app chrome: the primary icon nav rail down the left with the subtank rack-view page in the content area — the same shell composition the demo shows on :6060. Backend-free: a seeded session drives the nav rail and a fetch shim keeps the shell pollers quiet.",
      },
    },
  },
  render: () => (
    <FleetShellStoryDecorator>
      <SubtankDetail {...baseProps} />
    </FleetShellStoryDecorator>
  ),
};
