import type { Meta, StoryObj } from "@storybook/react-vite";
import UnlocatedHashboards from "./UnlocatedHashboards";

const meta = {
  title: "Shared/Hardware/Unlocated hashboards",
  component: UnlocatedHashboards,
  args: {
    boards: Array.from({ length: 28 }, (_, index) => ({
      id: String(index),
      identity: `aa:bb:cc:dd:ee:${String(index).padStart(2, "0")}`,
      serial: `CT1-${index}`,
      firmware: "v1.2.0",
      status: index === 0 ? "Needs attention" : "Hashing",
      hashrate: index === 0 ? "—" : "2.5 TH/s",
      temperature: "28.0 °C",
      issues: index === 0 ? ["Board over temperature"] : [],
    })),
  },
} satisfies Meta<typeof UnlocatedHashboards>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Default: Story = {};
export const Stale: Story = { args: { stale: true } };
export const Empty: Story = { args: { boards: [] } };
