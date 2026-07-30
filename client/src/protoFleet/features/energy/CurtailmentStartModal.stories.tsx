import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
  type CurtailmentResponseProfileOption,
  type CurtailmentStartModalMode,
  type CurtailmentStartModalVariant,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import type { FacilityFanDeviceOption } from "@/protoFleet/features/energy/FacilityFanSelectionModal";
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
  infrastructureDevices?: FacilityFanDeviceOption[];
  variant?: CurtailmentStartModalVariant;
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

const infrastructureDevices: FacilityFanDeviceOption[] = [
  {
    id: "31",
    siteId: "101",
    siteName: "Austin, TX",
    buildingName: "Building 1",
    name: "Fan Unit 1",
    deviceKind: "single_fan",
    fanCount: 1,
    enabled: true,
  },
  {
    id: "32",
    siteId: "101",
    siteName: "Austin, TX",
    buildingName: "Building 1",
    name: "Exhaust Fan Group",
    deviceKind: "fan_group",
    fanCount: 4,
    enabled: false,
  },
  {
    id: "33",
    siteId: "102",
    siteName: "Denver, CO",
    buildingName: "Building 2",
    name: "Denver Fan",
    deviceKind: "single_fan",
    fanCount: 1,
    enabled: true,
  },
];

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
  render: () => <ModalStory responseProfiles={responseProfiles} infrastructureDevices={infrastructureDevices} />,
};

export const WithPreview: Story = {
  name: "Fixed kW reduction preview",
  render: () => <ModalStory initialValues={configuredValues} preview={preview} responseProfiles={responseProfiles} />,
};

export const WithSelectedFan: Story = {
  name: "Fixed kW reduction with selected fan",
  render: () => (
    <ModalStory
      initialValues={{
        ...configuredValues,
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
      }}
      infrastructureDevices={infrastructureDevices}
      preview={{ ...preview, facilityFanDeviceCount: 1 }}
      responseProfiles={responseProfiles}
    />
  ),
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

export const ResponseProfileWithInfrastructure: Story = {
  name: "Response profile with infrastructure",
  render: () => (
    <ModalStory
      variant="responseProfile"
      infrastructureDevices={infrastructureDevices}
      initialValues={{
        ...configuredValues,
        facilityFanDeviceIds: ["31", "32"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
      }}
      preview={preview}
    />
  ),
};
