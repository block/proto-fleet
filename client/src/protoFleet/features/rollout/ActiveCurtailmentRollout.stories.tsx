import type { ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  curtailedCurtailmentEvent,
  curtailingCurtailmentEvent,
  restoredCurtailmentEvent,
  restoringCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus.fixtures";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  AnimatedCurtailmentBatchReviewSeriesInSitu,
  AnimatedCurtailmentInSitu,
  AnimatedCurtailmentPilotReviewSeriesInSitu,
  CurtailmentInSitu,
  CurtailmentStatusInSitu,
  EnergySurface,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  batchReviewCurtailmentEvent,
  inProgressCurtailmentEvent,
  pausedCurtailmentEvent,
  pilotGateCurtailmentEvent,
  scheduledCurtailmentEvent,
  stabilizingCurtailmentEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import type { RolloutEvent } from "@/protoFleet/features/rollout/rolloutTypes";

/**
 * Curtailment rollout lifecycle states rendered on the Energy page. This mirrors
 * the firmware and reboot in-situ lifecycle series.
 */
const meta = {
  title: "Proto Fleet/Rollout/Framework/Lifecycle/Curtailment",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The page shell provides its own MemoryRouter at /energy.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

const animatedSingleBatchCurtailmentEvent: RolloutEvent = {
  ...inProgressCurtailmentEvent,
  strategy: "allAtOnce",
  batchSize: undefined,
  batchIntervalSec: undefined,
  currentBatch: undefined,
  totalBatches: undefined,
  estimatedSecondsRemaining: 45,
  rollups: [{ phase: "inProgress", count: 240 }],
};

const curtailingRolloutContext: RolloutEvent = {
  ...inProgressCurtailmentEvent,
  title: curtailingCurtailmentEvent.reason,
  scopeLabel: curtailingCurtailmentEvent.scopeLabel,
  totalTargets: 18,
  excludedTargets: 0,
  batchSize: 10,
  batchIntervalSec: 120,
  currentBatch: 2,
  totalBatches: 2,
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 360, current: 40 },
      { label: "Power", unit: "power", baseline: 191.9, current: 132.5 },
      { label: "Avg temp", unit: "temperature", baseline: 64.0, current: 61.5 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.4 },
    ],
  },
  rollups: [
    { phase: "done", count: 16 },
    { phase: "inProgress", count: 2 },
  ],
};

const curtailedRolloutContext: RolloutEvent = {
  ...curtailingRolloutContext,
  state: "completed",
  currentBatch: 2,
  estimatedSecondsRemaining: 0,
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 360, current: 0 },
      { label: "Power", unit: "power", baseline: 191.9, current: 131.9 },
      { label: "Avg temp", unit: "temperature", baseline: 64.0, current: 60.8 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.36 },
    ],
  },
  rollups: [{ phase: "done", count: 18 }],
};

const restoringRolloutContext: RolloutEvent = {
  ...curtailedRolloutContext,
  curtailmentTelemetryPhase: "restore",
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 360, current: 160 },
      { label: "Power", unit: "power", baseline: 191.9, current: 132.5 },
      { label: "Avg temp", unit: "temperature", baseline: 64.0, current: 61.9 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.42 },
    ],
  },
  rollups: [
    { phase: "done", count: 8 },
    { phase: "queued", count: 10 },
  ],
};

const restoredRolloutContext: RolloutEvent = {
  ...curtailedRolloutContext,
  curtailmentTelemetryPhase: "restore",
  performance: {
    metrics: [
      { label: "Hashrate", unit: "hashrate", baseline: 360, current: 360 },
      { label: "Power", unit: "power", baseline: 191.9, current: 191.9 },
      { label: "Avg temp", unit: "temperature", baseline: 64.0, current: 63.8 },
      { label: "Efficiency", unit: "efficiency", baseline: 17.5, current: 17.48 },
    ],
  },
};

const animatedBatchesReviewCurtailmentEvent: RolloutEvent = {
  ...inProgressCurtailmentEvent,
  currentBatch: 1,
  reviewAfterEachBatch: true,
  rollups: [
    { phase: "inProgress", count: 60 },
    { phase: "queued", count: 180 },
  ],
};

const animatedPilotReviewCurtailmentEvent: RolloutEvent = {
  ...inProgressCurtailmentEvent,
  strategy: "pilotThenContinue",
  pilotSize: 30,
  currentBatch: 1,
  totalBatches: 2,
  rollups: [
    { phase: "inProgress", count: 30 },
    { phase: "queued", count: 210 },
  ],
};

function ScheduledCurtailmentStory(): ReactElement {
  return <EnergySurface event={null} scheduledEvent={scheduledCurtailmentEvent} />;
}

export const Scheduled: Story = {
  render: () => <ScheduledCurtailmentStory />,
};

export const Curtailing: Story = {
  render: () => <CurtailmentStatusInSitu event={curtailingCurtailmentEvent} rolloutEvent={curtailingRolloutContext} />,
};

export const CurtailingWithInfrastructure: Story = {
  name: "Curtailing with infrastructure",
  render: () => (
    <CurtailmentStatusInSitu
      event={{
        ...curtailingCurtailmentEvent,
        facilityFanDeviceCount: 2,
        fanOffSentAt: "2026-05-01T12:05:00.000Z",
      }}
      rolloutEvent={curtailingRolloutContext}
    />
  ),
};

export const WaitingForTelemetry: Story = {
  name: "Waiting for telemetry",
  render: () => <CurtailmentInSitu event={stabilizingCurtailmentEvent} />,
};

export const Paused: Story = {
  render: () => <CurtailmentInSitu event={pausedCurtailmentEvent} />,
};

export const BatchReview: Story = {
  name: "Batch review",
  render: () => <CurtailmentInSitu event={batchReviewCurtailmentEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <CurtailmentInSitu event={pilotGateCurtailmentEvent} />,
};

export const Curtailed: Story = {
  render: () => <CurtailmentStatusInSitu event={curtailedCurtailmentEvent} rolloutEvent={curtailedRolloutContext} />,
};

export const Restoring: Story = {
  render: () => <CurtailmentStatusInSitu event={restoringCurtailmentEvent} rolloutEvent={restoringRolloutContext} />,
};

export const RestoringWithInfrastructure: Story = {
  name: "Restoring with infrastructure",
  render: () => (
    <CurtailmentStatusInSitu
      event={{
        ...restoringCurtailmentEvent,
        facilityFanDeviceCount: 2,
        fanOnSentAt: "2026-05-01T12:08:00.000Z",
      }}
      rolloutEvent={restoringRolloutContext}
    />
  ),
};

export const Restored: Story = {
  render: () => <CurtailmentStatusInSitu event={restoredCurtailmentEvent} rolloutEvent={restoredRolloutContext} />,
};

export const AnimatedSingleBatch: Story = {
  name: "Animated single-batch dispatch",
  render: function renderAnimatedSingleBatch(): ReactElement {
    return <AnimatedCurtailmentInSitu base={animatedSingleBatchCurtailmentEvent} />;
  },
};

export const AnimatedBatchReviewSeries: Story = {
  name: "Animated batch-review dispatch",
  render: function renderAnimatedBatchReviewSeries(): ReactElement {
    return <AnimatedCurtailmentBatchReviewSeriesInSitu base={animatedBatchesReviewCurtailmentEvent} />;
  },
};

export const AnimatedPilotWithReview: Story = {
  name: "Animated pilot-review dispatch",
  render: function renderAnimatedPilotWithReview(): ReactElement {
    return <AnimatedCurtailmentPilotReviewSeriesInSitu base={animatedPilotReviewCurtailmentEvent} />;
  },
};
