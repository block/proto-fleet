import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
  type CurtailmentResponseProfileOption,
  type CurtailmentStartModalMode,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { withMockedMinerSelectionApis } from "@/protoFleet/stories/MockedMinerSelectionApis";

const meta = {
  title: "Proto Fleet/Energy/Plan Curtailment Modal",
  component: CurtailmentStartModal,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [withMockedMinerSelectionApis],
} satisfies Meta<typeof CurtailmentStartModal>;

export default meta;

type Story = StoryObj<typeof CurtailmentStartModal>;

interface ModalStoryProps {
  initialValues?: Partial<CurtailmentFormValues>;
  mode?: CurtailmentStartModalMode;
  preview?: CurtailmentPlanPreview;
  responseProfiles?: CurtailmentResponseProfileOption[];
}

const configuredValues: Partial<CurtailmentFormValues> = {
  targetKw: "40",
  curtailBatchSize: "8",
  curtailBatchIntervalSec: "30",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

const responseProfiles: CurtailmentResponseProfileOption[] = [
  {
    id: "standard-shed",
    label: "Standard shed",
    values: {
      curtailmentMode: "fixedKwReduction",
      targetKw: "50",
      curtailBatchSize: "20",
      curtailBatchIntervalSec: "60",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    },
  },
  {
    id: "emergency-shed",
    label: "Emergency shed",
    values: {
      curtailmentMode: "fullFleet",
      targetKw: "",
      curtailBatchSize: "60",
      curtailBatchIntervalSec: "30",
      restoreBatchSize: "20",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    },
  },
  {
    id: "partial-reduction",
    label: "Partial reduction",
    values: {
      curtailmentMode: "fixedKwReduction",
      targetKw: "2000",
      curtailBatchSize: "40",
      curtailBatchIntervalSec: "60",
      restoreBatchSize: "20",
      restoreIntervalSec: "120",
      includeMaintenance: true,
    },
  },
];

const preview: CurtailmentPlanPreview = {
  selectedMinerCount: 18,
  targetKw: 40,
  estimatedReductionKw: 45,
  curtailEstimate: "~1 minute",
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
  render: () => <ModalStory responseProfiles={responseProfiles} />,
};

export const WithPreview: Story = {
  name: "Fixed kW reduction preview",
  render: () => <ModalStory initialValues={configuredValues} preview={preview} responseProfiles={responseProfiles} />,
};

export const FullFleet: Story = {
  name: "Full shutdown preview",
  render: () => (
    <ModalStory
      initialValues={{ ...configuredValues, curtailmentMode: "fullFleet", targetKw: "" }}
      preview={{ ...preview, targetKw: 45 }}
    />
  ),
};

export const EditMode: Story = {
  name: "Edit mode",
  render: () => <ModalStory initialValues={configuredValues} preview={preview} mode="edit" />,
};
