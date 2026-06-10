import { type ReactElement, type ReactNode, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { sitesClient } from "@/protoFleet/api/clients";
import {
  ListSitesResponseSchema,
  SiteSchema,
  SiteWithCountsSchema,
} from "@/protoFleet/api/generated/sites/v1/sites_pb";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentPlanPreview,
  type CurtailmentStartModalMode,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };

const mutableSitesClient = sitesClient as MutableClient<typeof sitesClient>;

const storySites = [
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: 1n, name: "Austin" }),
    deviceCount: 18n,
    buildingCount: 1n,
    rackCount: 3n,
  }),
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: 2n, name: "Boise" }),
    deviceCount: 12n,
    buildingCount: 1n,
    rackCount: 2n,
  }),
];

const MockedCurtailmentModalApis = ({ children }: { children: ReactNode }) => {
  const [installed, setInstalled] = useState(false);

  useEffect(() => {
    const cleanup = installMockedCurtailmentModalApis();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- gate child render until the story API mock is installed
    setInstalled(true);
    return cleanup;
  }, []);

  if (!installed) return null;
  return <>{children}</>;
};

const installMockedCurtailmentModalApis = createRefCountedStoryMock(() => {
  const originalListSites = mutableSitesClient.listSites;

  mutableSitesClient.listSites = async () =>
    create(ListSitesResponseSchema, {
      sites: storySites,
    });

  return () => {
    mutableSitesClient.listSites = originalListSites;
  };
});

const withMockedCurtailmentModalApis = (Story: () => ReactNode) => (
  <MockedCurtailmentModalApis>
    <Story />
  </MockedCurtailmentModalApis>
);

const meta = {
  title: "Proto Fleet/Energy/Plan Curtailment Modal",
  component: CurtailmentStartModal,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [withMockedCurtailmentModalApis],
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
