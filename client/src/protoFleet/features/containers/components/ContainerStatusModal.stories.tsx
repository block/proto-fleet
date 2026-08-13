import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import type { ContainerFan } from "./ContainerOverview";
import ContainerStatusModal from "./ContainerStatusModal";

const runningFan: ContainerFan = { id: "f3", label: "Fan 3", on: true, speedPercent: 61, speedLabel: "2,870" };
const offFan: ContainerFan = { id: "f4", label: "Fan 4", on: false, speedPercent: 0, speedLabel: "0" };

/**
 * Container-side component-status glance opened from a fan card's (ⓘ) on the
 * overview (Item 11). Reuses the shared StatusModal framework's fan component
 * view — same icon, layout, and metrics grid the miner modal drills into.
 */
const meta: Meta<typeof ContainerStatusModal> = {
  title: "Proto Containers/Overview/Fan Status",
  component: ContainerStatusModal,
  parameters: { layout: "centered" },
};

export default meta;
type Story = StoryObj<typeof ContainerStatusModal>;

const Interactive = ({ initialFan }: { initialFan: ContainerFan }) => {
  const [fan, setFan] = useState<ContainerFan | null>(initialFan);
  return (
    <div style={{ minWidth: "500px", minHeight: "400px" }}>
      <button onClick={() => setFan(initialFan)} style={{ marginBottom: "1rem" }}>
        Open glance
      </button>
      <ContainerStatusModal fan={fan} onClose={() => setFan(null)} />
    </div>
  );
};

export const RunningFan: Story = {
  name: "Running fan",
  render: () => <Interactive initialFan={runningFan} />,
};

export const PoweredOffFan: Story = {
  name: "Powered off fan",
  render: () => <Interactive initialFan={offFan} />,
};
