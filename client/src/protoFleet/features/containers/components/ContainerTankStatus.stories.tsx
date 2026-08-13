import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import type { ContainerTank } from "./ContainerOverview";
import ContainerStatusModal from "./ContainerStatusModal";
import type { TankModuleState } from "./TankModuleGrid";

/** 16 modules with `attention` needing-attention entries, rest healthy. */
function modules(total: number, attention: number): TankModuleState[] {
  return Array.from({ length: total }, (_, i) => (i < attention ? "attention" : "healthy"));
}

const runningTank: ContainerTank = {
  id: "t2",
  label: "Tank 2",
  on: true,
  cols: 8,
  rows: 2,
  modules: modules(16, 2),
  stats: ["14/16 modules", "67.1°", "11.8 kW"],
  tempLabel: "67.1°",
  powerLabel: "11.8 kW",
};

const offTank: ContainerTank = {
  id: "t6",
  label: "Tank 6",
  on: false,
  cols: 8,
  rows: 2,
  modules: modules(16, 0),
  stats: ["0/16 modules", "—", "0.0 kW"],
  tempLabel: "—",
  powerLabel: "0.0 kW",
};

/**
 * Container-side tank-level status glance opened from a tank card's (ⓘ) on the
 * overview (Item 11, tank half). Reuses the shared StatusModal framework's
 * first-class "tank" component view — same layout and metrics grid the miner
 * modal drills into, with the liquid-cooling icon. The (ⓘ) is the quick
 * tank-level summary (modules healthy/attention/offline + temp/power); the full
 * drill-down is the Subtank detail page (reached by clicking the tank card
 * body).
 */
const meta: Meta<typeof ContainerStatusModal> = {
  title: "Proto Containers/Overview/Tank Status",
  component: ContainerStatusModal,
  parameters: { layout: "centered" },
};

export default meta;
type Story = StoryObj<typeof ContainerStatusModal>;

const Interactive = ({ initialTank }: { initialTank: ContainerTank }) => {
  const [tank, setTank] = useState<ContainerTank | null>(initialTank);
  return (
    <div style={{ minWidth: "500px", minHeight: "400px" }}>
      <button onClick={() => setTank(initialTank)} style={{ marginBottom: "1rem" }}>
        Open glance
      </button>
      <ContainerStatusModal tank={tank} onClose={() => setTank(null)} />
    </div>
  );
};

export const RunningTank: Story = {
  name: "Running tank (needs attention)",
  render: () => <Interactive initialTank={runningTank} />,
};

export const PoweredOffTank: Story = {
  name: "Powered off tank",
  render: () => <Interactive initialTank={offTank} />,
};
