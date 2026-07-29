// Proto container-module hashboard stories.
//
// These render the REAL production components for a Proto container module
// (26x12 serpentine ASIC grid) so the module-specific layout can be reviewed
// alongside the rig path. Three components branch on `useIsProtoModule`, which
// keys off `systemInfo.model` (interim "CU…" signal) — a global store flag, not
// a prop — so a single seeded module drives all of them:
//   - HashboardStatusCard / AsicTablePreview → icon-less card, 16px rows
//   - AsicTable / AsicButton                 → rigid grid, 4px gutters
//   - AsicPopover                            → serpentine ASIC label (on click)
//
// The seed decorator sets the module up on mount and calls resetDeviceData on
// unmount so the Proto flag never leaks into sibling (rig) stories.
import { type ReactNode, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ComponentSection from "../ComponentSection";
import HashboardStatusCard from "../HashboardStatusCard/HashboardStatusCard";
import HashboardTemperature from "./HashboardTemperature";
import { PROTO_HASHBOARD_COLUMNS, PROTO_HASHBOARD_ROWS } from "@/protoOS/store";
import useMinerStore from "@/protoOS/store/useMinerStore";

const N = PROTO_HASHBOARD_COLUMNS; // 26
const ROWS = PROTO_HASHBOARD_ROWS; // 12

const BOARDS = [
  { serial: "CU1-HB-1", slot: 1, profile: "cool" as const },
  { serial: "CU1-HB-2", slot: 2, profile: "cool" as const },
  { serial: "CU1-HB-3", slot: 3, profile: "warm" as const },
];

// Deterministic pseudo-temperature so the heatmap is stable across renders.
// "cool" boards sit in the blue band; the "warm" board has a soft central
// cluster that ramps through orange into a few red cells — a realistic spread,
// not a stress test.
function temp(profile: "cool" | "warm", row: number, col: number): number {
  if (profile === "cool") {
    return 28 + ((row * 5 + col * 2) % 8); // ~28–36°C
  }
  const rowHeat = 1 - Math.abs(row - ROWS / 2) / (ROWS / 2);
  const colHeat = 1 - Math.abs(col - N / 2) / (N / 2);
  const peak = Math.max(0, rowHeat) * Math.max(0, colHeat); // 0 (edge) → 1 (center)
  return Math.round(40 + peak * 48); // ~40°C edges → ~88°C center
}

const SeedStore = ({ children }: { children: ReactNode }) => {
  useEffect(() => {
    const store = useMinerStore.getState();

    // Mark this device as a Proto container module (model "CU1" -> useIsProtoModule).
    store.systemInfo.setSystemInfo({
      manufacturer: "Proto",
      model: "CU1",
      product_name: "Proto CU1",
      cb_sn: "CU1-CB-0001",
    });

    BOARDS.forEach(({ serial, slot, profile }) => {
      const asicIds: string[] = [];
      const asics: { id: string; hashboardSerial: string; row: number; column: number; index: number }[] = [];
      const telemetry = new Map<string, { id: string; temperature: { latest: { value: number; units: "C" } } }>();

      let index = 0;
      for (let row = 0; row < ROWS; row++) {
        for (let col = 0; col < N; col++) {
          const id = `${serial}-asic-${index}`;
          asicIds.push(id);
          asics.push({ id, hashboardSerial: serial, row, column: col, index });
          telemetry.set(id, { id, temperature: { latest: { value: temp(profile, row, col), units: "C" } } });
          index++;
        }
      }

      store.hardware.addHashboard({ serial, slot, bay: 0, asicIds });
      store.hardware.batchAddAsics(asics);

      useMinerStore.setState((state) => {
        telemetry.forEach((t, id) => {
          state.telemetry.asics.set(id, { ...(state.telemetry.asics.get(id) || {}), ...t });
        });
      });

      store.telemetry.updateHashboardTemperatures(
        serial,
        { value: profile === "warm" ? 59.5 : 41.2, units: "C" },
        { value: profile === "warm" ? 62.1 : 43.8, units: "C" },
        { value: profile === "warm" ? 55.2 : 30.1, units: "C" },
        { value: profile === "warm" ? 88.0 : 35.7, units: "C" },
      );
    });

    // Clear the Proto module (and its flag) so sibling rig stories start clean.
    return () => {
      useMinerStore.getState().resetDeviceData();
    };
  }, []);

  return <>{children}</>;
};

const meta: Meta = {
  title: "Proto OS/Diagnostic/Proto Module Hashboard",
  decorators: [
    (Story) => (
      <MemoryRouter>
        <SeedStore>
          <Story />
        </SeedStore>
      </MemoryRouter>
    ),
  ],
  parameters: { withRouter: false, layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof meta>;

// Board-card heatmaps framed in the real Diagnostics "Hashboards" section
// (ComponentSection + HashboardStatusCard + AsicTablePreview): icon-less cards
// with 16px ASIC rows.
export const BoardCards: Story = {
  render: () => (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto max-w-[1100px]">
        {/* Mirror of DiagnosticView's page header + HashboardsSection */}
        <div className="flex flex-col items-start gap-3 pb-6 tablet:flex-row tablet:items-center">
          <div className="grow text-heading-300">Diagnostics</div>
        </div>
        <ComponentSection title="Hashboards">
          <div className="grid gap-1 tablet:grid-cols-2 desktop:grid-flow-col desktop:grid-cols-3 desktop:grid-rows-3">
            {BOARDS.map(({ serial }) => (
              <HashboardStatusCard key={serial} serialNumber={serial} />
            ))}
          </div>
        </ComponentSection>
      </div>
    </div>
  ),
};

// Expanded grid framed in the real module-detail chrome: the full-screen
// HashboardTemperature screen (header, hashboard selector, Stats, metric
// SegmentedControl, front/rear rails) wrapping the rigid AsicTable grid.
// Click any cell to see the serpentine ASIC label in the popover.
export const ExpandedGrid: Story = {
  render: () => <HashboardTemperature serial="CU1-HB-3" />,
};
