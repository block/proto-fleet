import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentStartModal from "@/protoFleet/features/energy/CurtailmentStartModal";
import { mockPreview, storybookCurtailmentFormValues } from "@/protoFleet/features/energy/fixtures";

const meta: Meta<typeof CurtailmentStartModal> = {
  title: "Proto Fleet/Energy",
  component: CurtailmentStartModal,
  parameters: {
    layout: "fullscreen",
  },
};

export default meta;

type Story = StoryObj<typeof CurtailmentStartModal>;

function ModalStory(): ReactElement {
  const [open, setOpen] = useState(true);

  return (
    <div className="min-h-screen bg-surface-base">
      <CurtailmentStartModal
        open={open}
        onDismiss={() => setOpen(false)}
        onPreviewCurtailmentPlan={async () => mockPreview}
        onStartCurtailment={async () => undefined}
        initialValues={{ ...storybookCurtailmentFormValues, reason: "" }}
      />
    </div>
  );
}

export const PlanCurtailmentModal: Story = {
  name: "Plan curtailment modal",
  render: () => <ModalStory />,
};
