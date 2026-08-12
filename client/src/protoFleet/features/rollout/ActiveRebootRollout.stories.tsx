import { type ReactElement, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { ActiveRolloutBanner } from "@/protoFleet/features/rollout/ActiveRolloutBanner";
import ActiveRolloutStatus from "@/protoFleet/features/rollout/ActiveRolloutStatus";
import {
  AnimatedRebootBatchReviewSeriesInSitu,
  AnimatedRebootInSitu,
  AnimatedRebootPilotReviewSeriesInSitu,
  FleetSurface,
  RebootInSitu,
} from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import {
  batchedRebootConfig,
  batchReviewRebootEvent,
  completedRebootEvent,
  inProgressRebootEvent,
  pausedRebootEvent,
  pilotGateRebootEvent,
  scheduledRebootEvent,
  stabilizingRebootEvent,
} from "@/protoFleet/features/rollout/rollout.fixtures";
import RolloutConfigModal from "@/protoFleet/features/rollout/RolloutConfigModal";
import type { RolloutEvent, RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";

/**
 * Reboot rollout lifecycle states rendered on the Fleet page. Reboot is a bulk
 * action, so these stories use the Fleet page as the in-product home.
 */
const meta = {
  title: "Proto Fleet/Rollout/Framework/Lifecycle/Reboot",
  component: ActiveRolloutStatus,
  parameters: {
    layout: "fullscreen",
    // The page shell provides its own MemoryRouter at /fleet.
    withRouter: false,
  },
} satisfies Meta<typeof ActiveRolloutStatus>;

export default meta;

type Story = StoryObj<typeof ActiveRolloutStatus>;

const noop = (): void => undefined;

const animatedSingleBatchRebootEvent: RolloutEvent = {
  ...inProgressRebootEvent,
  strategy: "allAtOnce",
  batchSize: undefined,
  batchIntervalSec: undefined,
  currentBatch: undefined,
  totalBatches: undefined,
  estimatedSecondsRemaining: 60,
  rollups: [{ phase: "inProgress", count: 40 }],
};

const animatedBatchesReviewRebootEvent: RolloutEvent = {
  ...inProgressRebootEvent,
  currentBatch: 1,
  reviewAfterEachBatch: true,
  rollups: [
    { phase: "inProgress", count: 10 },
    { phase: "queued", count: 30 },
  ],
};

const animatedPilotReviewRebootEvent: RolloutEvent = {
  ...inProgressRebootEvent,
  strategy: "pilotThenContinue",
  pilotSize: 5,
  currentBatch: 1,
  totalBatches: 2,
  rollups: [
    { phase: "inProgress", count: 5 },
    { phase: "queued", count: 35 },
  ],
};

function ScheduledRebootStory(): ReactElement {
  const [configOpen, setConfigOpen] = useState(false);
  const [showScheduledBanner, setShowScheduledBanner] = useState(true);
  const [config, setConfig] = useState<RolloutPlanConfig>({
    ...batchedRebootConfig,
    scheduleType: "scheduleForLater",
    scheduledStartAt: scheduledRebootEvent.scheduledStartAt,
  });
  const [startDate, setStartDate] = useState<Date | undefined>(new Date("2026-08-14T14:00:00"));
  const [startTime, setStartTime] = useState("14:00");

  return (
    <>
      <FleetSurface
        event={null}
        rolloutBanner={
          showScheduledBanner ? (
            <ActiveRolloutBanner event={scheduledRebootEvent} onManage={() => setConfigOpen(true)} />
          ) : null
        }
      />
      {configOpen ? (
        <RolloutConfigModal
          title="Manage scheduled reboot"
          description="40 miners in Rack A3"
          config={config}
          onConfigChange={setConfig}
          scopeTargets={[{ label: "Rack", value: "Rack A3", onClick: noop }]}
          inScopeCount={40}
          startDate={startDate}
          onStartDateChange={setStartDate}
          startTime={startTime}
          onStartTimeChange={setStartTime}
          submitLabel="Save changes"
          onDismiss={() => setConfigOpen(false)}
          onSubmit={() => setConfigOpen(false)}
          onCancelScheduled={() => {
            setConfigOpen(false);
            setShowScheduledBanner(false);
          }}
        />
      ) : null}
    </>
  );
}

export const Scheduled: Story = {
  render: () => <ScheduledRebootStory />,
};

export const InProgress: Story = {
  name: "In progress",
  render: () => <RebootInSitu event={inProgressRebootEvent} />,
};

export const WaitingForTelemetry: Story = {
  name: "Waiting for telemetry",
  render: () => <RebootInSitu event={stabilizingRebootEvent} />,
};

export const Paused: Story = {
  render: () => <RebootInSitu event={pausedRebootEvent} />,
};

export const BatchReview: Story = {
  name: "Batch review",
  render: () => <RebootInSitu event={batchReviewRebootEvent} />,
};

export const PilotReview: Story = {
  name: "Pilot review",
  render: () => <RebootInSitu event={pilotGateRebootEvent} />,
};

export const Completed: Story = {
  render: () => <RebootInSitu event={completedRebootEvent} />,
};

export const AnimatedSingleBatch: Story = {
  name: "Animated single batch",
  render: function renderAnimatedSingleBatch(): ReactElement {
    return <AnimatedRebootInSitu base={animatedSingleBatchRebootEvent} />;
  },
};

export const AnimatedBatchReviewSeries: Story = {
  name: "Animated batch review series",
  render: function renderAnimatedBatchReviewSeries(): ReactElement {
    return <AnimatedRebootBatchReviewSeriesInSitu base={animatedBatchesReviewRebootEvent} />;
  },
};

export const AnimatedPilotWithReview: Story = {
  name: "Animated pilot with review",
  render: function renderAnimatedPilotWithReview(): ReactElement {
    return <AnimatedRebootPilotReviewSeriesInSitu base={animatedPilotReviewRebootEvent} />;
  },
};
