import type { Meta, StoryObj } from "@storybook/react-vite";

import { restoreIncompleteCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus.fixtures";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  CurtailmentInSitu,
  CurtailmentStatusInSitu,
  FirmwareInSitu,
  RebootInSitu,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  completedWithFailuresFirmwareEvent,
  completedWithFailuresRebootEvent,
  inProgressWithErrorsCurtailmentEvent,
  inProgressWithErrorsFirmwareEvent,
  inProgressWithErrorsRebootEvent,
  pilotReviewWithErrorsFirmwareEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import type { RolloutEvent } from "@/protoFleet/features/rollout/rolloutTypes";

/**
 * Error-state examples live separately from the core lifecycle series so the
 * main in-situ stories stay focused on nominal rollout workflow.
 */
const meta = {
  title: "Proto Fleet/Rollout/Framework/Error Cases",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    withRouter: false,
    docs: {
      description: {
        component:
          "Consolidated rollout error-state examples. Keep these here instead of repeating failure variants across every lifecycle series.",
      },
    },
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

const restoreIncompleteRolloutContext: RolloutEvent = {
  ...inProgressWithErrorsCurtailmentEvent,
  curtailmentTelemetryPhase: "restore",
  state: "completedWithFailures",
  title: restoreIncompleteCurtailmentEvent.reason,
  scopeLabel: restoreIncompleteCurtailmentEvent.scopeLabel,
  totalTargets: 18,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 120,
  currentBatch: 2,
  totalBatches: 2,
  estimatedSecondsRemaining: 0,
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 360, current: 340 },
      { label: "Power", unit: "power", baseline: 191.9, current: 135.2 },
      { label: "Avg temp", unit: "temperature", baseline: 64.0, current: 61.2 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.43 },
    ],
  },
  errors: [
    {
      id: "curtailment-restore-failed",
      message: "Miner did not restore power",
      impactedMiners: ["b03-s21-03"],
    },
  ],
  rollups: [
    { phase: "done", count: 17 },
    { phase: "failed", count: 1 },
  ],
};

export const FirmwareInProgressWithErrors: Story = {
  name: "Firmware in progress with errors",
  render: () => <FirmwareInSitu event={inProgressWithErrorsFirmwareEvent} />,
};

export const FirmwareReviewWithErrors: Story = {
  name: "Firmware review with errors",
  render: () => <FirmwareInSitu event={pilotReviewWithErrorsFirmwareEvent} />,
};

export const FirmwareCompletedWithFailures: Story = {
  name: "Firmware completed with failures",
  render: () => <FirmwareInSitu event={completedWithFailuresFirmwareEvent} />,
};

export const RebootInProgressWithErrors: Story = {
  name: "Reboot in progress with errors",
  render: () => <RebootInSitu event={inProgressWithErrorsRebootEvent} />,
};

export const RebootCompletedWithFailures: Story = {
  name: "Reboot completed with failures",
  render: () => <RebootInSitu event={completedWithFailuresRebootEvent} />,
};

export const CurtailmentInProgressWithErrors: Story = {
  name: "Curtailment in progress with errors",
  render: () => <CurtailmentInSitu event={inProgressWithErrorsCurtailmentEvent} />,
};

export const CurtailmentCompletedWithFailures: Story = {
  name: "Curtailment restore incomplete",
  render: () => (
    <CurtailmentStatusInSitu event={restoreIncompleteCurtailmentEvent} rolloutEvent={restoreIncompleteRolloutContext} />
  ),
};
