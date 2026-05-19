import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
} from "@/protoFleet/features/energy/CurtailmentStartModal";

const meta = {
  title: "Proto Fleet/Energy/Plan Curtailment Modal",
  component: CurtailmentStartModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof CurtailmentStartModal>;

export default meta;

type Story = StoryObj<typeof CurtailmentStartModal>;
type ModalStoryProps = {
  initialValues?: Partial<CurtailmentFormValues>;
  preview?: CurtailmentPlanPreview;
};

const configuredValues: Partial<CurtailmentFormValues> = {
  targetKw: "40",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 40,
  estimatedReductionKw: 45,
  restoreEstimate: "~2 minutes",
  scopeLabel: "across the fleet",
};

function ModalStory(props: ModalStoryProps): ReactElement {
  const [open, setOpen] = useState(true);

  return (
    <div className="min-h-screen bg-surface-base">
      <CurtailmentStartModal open={open} onDismiss={() => setOpen(false)} onSubmit={() => setOpen(false)} {...props} />
    </div>
  );
}

export const Empty: Story = {
  render: () => <ModalStory />,
};

export const WithPreview: Story = {
  name: "Fixed kW reduction preview",
  render: () => <ModalStory initialValues={configuredValues} preview={preview} />,
};
