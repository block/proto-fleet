import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";

const meta = {
  title: "Proto Fleet/Energy/Curtailment History",
  component: CurtailmentHistory,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-8">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof CurtailmentHistory>;

export default meta;

type Story = StoryObj<typeof CurtailmentHistory>;

function CurtailmentHistoryStory(): ReactElement {
  return (
    <CurtailmentHistory
      events={mockCurtailmentHistoryEvents}
      activeEventId="curt-1042"
      pageSize={2}
      onManageActiveEvent={() => undefined}
      onStopActiveEvent={() => undefined}
    />
  );
}

export const Default: Story = {
  render: () => <CurtailmentHistoryStory />,
};

export const Empty: Story = {
  args: {
    events: [],
  },
};
