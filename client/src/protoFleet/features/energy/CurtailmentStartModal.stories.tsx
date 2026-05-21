import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
  type CurtailmentStartModalMode,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

const meta = {
  title: "Proto Fleet/Energy/Plan Curtailment Modal",
  component: CurtailmentStartModal,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof CurtailmentStartModal>;

export default meta;

type Story = StoryObj<typeof CurtailmentStartModal>;

interface ModalStoryProps {
  initialValues?: Partial<CurtailmentFormValues>;
  mode?: CurtailmentStartModalMode;
  preview?: CurtailmentPlanPreview;
}

const configuredValues: Partial<CurtailmentFormValues> = {
  targetKw: "40",
  minDurationSec: "300",
  maxDurationSec: "1800",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 40,
  estimatedReductionKw: 45,
  curtailEstimate: "5 minutes - 30 minutes",
  restoreEstimate: "~2 minutes",
  scopeLabel: "across the fleet",
};

function ModalStory(props: ModalStoryProps): ReactElement {
  const [open, setOpen] = useState(true);
  const [showStopDialog, setShowStopDialog] = useState(false);

  function closeStopDialog(): void {
    setShowStopDialog(false);
  }

  function handleConfirmStop(): void {
    closeStopDialog();
    setOpen(false);
  }

  return (
    <div className="min-h-screen bg-surface-base">
      <CurtailmentStartModal
        open={open}
        onDismiss={() => setOpen(false)}
        onSubmit={() => setOpen(false)}
        {...props}
        onStopCurtailment={props.mode === "edit" ? () => setShowStopDialog(true) : undefined}
      />
      <CurtailmentStopConfirmationDialog
        open={showStopDialog}
        action="stopCurtailment"
        onCancel={closeStopDialog}
        onConfirm={handleConfirmStop}
      />
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

export const EditMode: Story = {
  name: "Edit mode",
  render: () => <ModalStory initialValues={configuredValues} preview={preview} mode="edit" />,
};
