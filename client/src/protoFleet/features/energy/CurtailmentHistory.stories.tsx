import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

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
  const [showStopDialog, setShowStopDialog] = useState(false);

  function closeStopDialog(): void {
    setShowStopDialog(false);
  }

  function openStopDialog(): void {
    setShowStopDialog(true);
  }

  return (
    <>
      <CurtailmentHistory
        events={mockCurtailmentHistoryEvents}
        activeEventId="curt-1042"
        pageSize={2}
        onManageActiveEvent={() => undefined}
        onStopActiveEvent={openStopDialog}
      />
      <CurtailmentStopConfirmationDialog
        open={showStopDialog}
        action="stopCurtailment"
        onCancel={closeStopDialog}
        onConfirm={closeStopDialog}
      />
    </>
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
