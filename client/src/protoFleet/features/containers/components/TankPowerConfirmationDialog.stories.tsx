import type { Meta, StoryObj } from "@storybook/react";
import TankPowerConfirmationDialog from "./TankPowerConfirmationDialog";

const meta: Meta<typeof TankPowerConfirmationDialog> = {
  title: "Proto Containers/Overview/Tank Power Confirmation",
  component: TankPowerConfirmationDialog,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Confirmation dialog gating a tank power toggle. The tank switch drives the tank's PDU — cutting or restoring line power to every module inside it — so it is not a soft mining pause. The dialog spells out the PDU semantics before the destructive remote action fires, reusing the shared Dialog + DialogIcon pattern (mirrors the curtailment stop/restore confirmation). Powering off is a danger action (critical icon); powering on is a primary action (success icon).",
      },
    },
  },
  args: {
    open: true,
    onCancel: () => {},
    onConfirm: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof TankPowerConfirmationDialog>;

export const PowerOff: Story = {
  name: "Power off (PDU cut warning)",
  args: { label: "Tank 1", turningOn: false },
};

export const PowerOn: Story = {
  name: "Power on (restore)",
  args: { label: "Tank 6", turningOn: true },
};
