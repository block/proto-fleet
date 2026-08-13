import type { Meta, StoryObj } from "@storybook/react";
import { FleetShellStoryDecorator } from "./FleetShellStoryDecorator";
import type { TankModuleState } from "./TankModuleGrid";
import { toTankRackView } from "./tankRackView";
import { RackHealthModule } from "@/protoFleet/features/fleetManagement/components/RackHealthModule";
import Breadcrumb from "@/shared/components/Breadcrumb";

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

// A 16-module tank reads as 8 columns × 2 rows in the rack-detail grid.
const TANK_COLS = 8;
const TANK_ROWS = 2;
const MODULES_PER_TANK = TANK_COLS * TANK_ROWS;

interface TankSeed {
  label: string;
  on: boolean;
  attention: number;
  seed: number;
  /** Footer readouts, e.g. ["65.5°", "12.3 kW"]. */
  readouts: string[];
}

// The same six tanks the container overview shows (Tank 6 powered off), so the
// two views read as the same container from different angles.
const tankSeeds: TankSeed[] = [
  { label: "Tank 1", on: true, attention: 0, seed: 11, readouts: ["65.5°", "12.3 kW"] },
  { label: "Tank 2", on: true, attention: 2, seed: 23, readouts: ["67.1°", "11.8 kW"] },
  { label: "Tank 3", on: true, attention: 1, seed: 37, readouts: ["64.2°", "11.5 kW"] },
  { label: "Tank 4", on: true, attention: 3, seed: 41, readouts: ["68.9°", "11.1 kW"] },
  { label: "Tank 5", on: true, attention: 0, seed: 53, readouts: ["65.0°", "12.4 kW"] },
  { label: "Tank 6", on: false, attention: 0, seed: 67, readouts: ["—", "0.0 kW"] },
];

/**
 * Container tanks laid out as Fleet racks: each tank rendered through the exact
 * rack-detail RackHealthModule (RackDetailGrid slot grid + status breakdown),
 * fed by the pure toTankRackView adapter. This is the "tanks as racks" view —
 * the same modules the overview draws as tank cards, shown in the fleet's
 * rack-view format so a container reads like a rack room. Presentational and
 * prop-driven (prototype-first).
 */
function TanksAsRacks() {
  return (
    <div className="flex flex-col gap-10 p-6 laptop:p-10" data-testid="tanks-as-racks">
      <div className="flex flex-col gap-3">
        <Breadcrumb
          segments={[
            { label: "Sites", to: "/fleet/sites" },
            { label: "Kati 1A", to: "/sites/1" },
            { label: "Container 01" },
          ]}
          testId="tanks-as-racks-breadcrumb"
        />
        <div className="text-heading-300 text-text-primary">Container 01 — Tanks</div>
        <div className="text-300 text-text-primary-50">
          Each tank rendered in the Fleet rack-view format (16 modules as an 8 × 2 slot grid).
        </div>
      </div>

      <div className="flex flex-col gap-8">
        {tankSeeds.map((tank) => {
          const view = toTankRackView({
            cols: TANK_COLS,
            rows: TANK_ROWS,
            modules: makeModules(MODULES_PER_TANK, tank.attention, tank.seed),
            on: tank.on,
            numberingOrigin: "bottom-left",
          });
          // Match the overview tank card's "healthy/total" framing (needs-attention
          // modules are powered but excluded from the numerator); the breakdown
          // panel to the right carries the full healthy/attention/offline split.
          const healthy = tank.on ? view.hashingCount : 0;
          return (
            <section key={tank.label} className="flex flex-col gap-3" data-testid={`tank-as-rack-${tank.label}`}>
              <div className="flex items-baseline gap-3">
                <div className="text-heading-200 text-text-primary">{tank.label}</div>
                <div className="text-200 text-text-primary-50">
                  {healthy}/{MODULES_PER_TANK} modules, {tank.readouts.join(", ")}
                </div>
              </div>
              <RackHealthModule
                rows={view.rows}
                cols={view.cols}
                slotStates={view.slotStates}
                numberingOrigin="bottom-left"
                hashingCount={view.hashingCount}
                needsAttentionCount={view.needsAttentionCount}
                offlineCount={view.offlineCount}
                sleepingCount={0}
              />
            </section>
          );
        })}
      </div>
    </div>
  );
}

const meta: Meta<typeof TanksAsRacks> = {
  title: "Proto Containers/Overview/Tanks as Racks",
  component: TanksAsRacks,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Container tanks rendered in the Fleet rack-view format: each of the container's six tanks is drawn with the exact rack-detail RackHealthModule (RackDetailGrid slot grid + status breakdown panel), fed by the pure toTankRackView adapter that maps a tank's 16 modules to an 8 × 2 slot grid (module bar N = slot N). This is the tank analogue of the fleet's rack list — the same modules the overview draws as distinct tank cards, shown here as racks so a container reads like a rack room. Tank 6 is powered off, so its populated modules read offline. Presentational and prop-driven (prototype-first).",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof TanksAsRacks>;

export const Default: Story = {};

// ----------------------------------------------------------------------------
// Tanks-as-racks inside the REAL Fleet app chrome (icon nav rail), via the
// shared FleetShellStoryDecorator — the same shell composition the demo shows
// on :6060.
// ----------------------------------------------------------------------------
export const InFleetShell: Story = {
  name: "In Fleet Shell (icon nav)",
  parameters: {
    docs: {
      description: {
        story:
          "Tanks-as-racks mounted inside the REAL Fleet app chrome: the primary icon nav rail down the left with the tanks-as-racks page in the content area — the same shell composition the demo shows on :6060. Backend-free: a seeded session drives the nav rail and a fetch shim keeps the shell pollers quiet.",
      },
    },
  },
  render: () => (
    <FleetShellStoryDecorator>
      <TanksAsRacks />
    </FleetShellStoryDecorator>
  ),
};
