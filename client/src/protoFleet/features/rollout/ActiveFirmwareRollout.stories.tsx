import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  type Rollout,
  RolloutBehaviorSchema,
  RolloutEvidenceSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import ActiveUpdatesMonitor from "@/protoFleet/features/settings/components/ReleaseChannels/ActiveUpdatesMonitor";
import {
  ConcurrentUpdates,
  FirmwareMonitorPage,
  SingleBatchReview,
  SingleInProgress,
  SinglePilotReview,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ActiveUpdatesMonitor.stories";
import {
  batchedAutoBehavior,
  gatedRigRollout,
  pausedRigRollout,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ReleaseChannels.fixtures";
import { Completed as CompletedDetail } from "@/protoFleet/features/settings/components/ReleaseChannels/RolloutDetailModal.stories";

// Preserve the existing design-reference URLs while rendering the production
// views. Scheduled and animated prototypes have no production equivalent.
const meta = {
  title: "Proto Fleet/Rollout/Framework/Lifecycle/Firmware",
  component: ActiveUpdatesMonitor,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Production firmware views with fixed server snapshots. Actions settle locally without issuing RPCs.",
      },
    },
  },
} satisfies Meta<typeof ActiveUpdatesMonitor>;

export default meta;
type Story = StoryObj<typeof ActiveUpdatesMonitor>;

// The healthy pilot is waiting out its configured stabilization period.
// Reuse its device list and evidence, and show the server-provided countdown.
const waitingForTelemetry: Rollout = {
  ...gatedRigRollout,
  state: RolloutState.STABILIZING_TELEMETRY,
  behavior: create(RolloutBehaviorSchema, {
    ...create(RolloutBehaviorSchema, gatedRigRollout.behavior),
    autoContinueOnHealthyTelemetry: true,
    stabilizationSeconds: batchedAutoBehavior.stabilizationSeconds,
    thresholds: batchedAutoBehavior.thresholds,
  }),
  evidence: create(RolloutEvidenceSchema, {
    ...create(RolloutEvidenceSchema, gatedRigRollout.evidence),
    holdReason: "Waiting for telemetry to stabilize",
    stabilizationRemainingSeconds: 90,
    readyToAdvance: false,
  }),
};

export const ConcurrentRollouts: Story = { ...ConcurrentUpdates, name: "Concurrent rollouts" };
export const InProgress: Story = { ...SingleInProgress, name: "In progress" };
export const WaitingForTelemetry: Story = {
  name: "Waiting for telemetry",
  render: () => <FirmwareMonitorPage rollouts={[waitingForTelemetry]} />,
};
export const Paused: Story = {
  render: () => <FirmwareMonitorPage rollouts={[pausedRigRollout]} />,
};
export const BatchReview: Story = { ...SingleBatchReview, name: "Batch review" };
export const PilotReview: Story = { ...SinglePilotReview, name: "Pilot review" };
export const Completed = { ...CompletedDetail };
