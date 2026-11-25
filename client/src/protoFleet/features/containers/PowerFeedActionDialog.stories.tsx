import type { Meta, StoryObj } from "@storybook/react";
import PowerFeedActionDialog from "./PowerFeedActionDialog";

const meta: Meta<typeof PowerFeedActionDialog> = {
  title: "Proto Fleet/Containers/Power Feed Action",
  component: PowerFeedActionDialog,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Confirms one write to a module's mapped power feed. It names the outlet and module, says that feed power is separate from mining, and gives the cycle hold. It sends at most once and reports the controller's result.",
      },
    },
  },
  args: {
    moduleId: 91n,
    feedId: "pdu-1-outlet-05",
    containerName: "North CT1",
    tank: 1,
    module: 2,
    cycleHoldSeconds: 30,
    onClose: () => {},
    onSettled: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof PowerFeedActionDialog>;

export const Cycle: Story = { args: { action: "cycle" } };
export const TurnOff: Story = { args: { action: "turn_off" } };
export const TurnOn: Story = { args: { action: "turn_on" } };
export const ClearOverride: Story = { args: { action: "clear_override" } };
