import { type ComponentProps, type ReactElement, useEffect, useMemo, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveCurtailmentStatus, {
  type ActiveCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  curtailedCurtailmentEvent,
  curtailingCurtailmentEvent,
  restoredCurtailmentEvent,
  restoreIncompleteCurtailmentEvent,
  restoringCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus.fixtures";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

const meta = {
  title: "Proto Fleet/Energy/Active Curtailment Status",
  component: ActiveCurtailmentStatus,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-8">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ActiveCurtailmentStatus>;

export default meta;

type Story = StoryObj<typeof ActiveCurtailmentStatus>;
type AnimationPhase = "shed" | "fanCurtailDelay" | "curtailed" | "fanRestoreDelay" | "restore" | "restored";
type AnimatedEventPhase = "shed" | "restore" | "restored";

interface BuildAnimatedEventArgs {
  phase: AnimatedEventPhase;
  progressPercent: number;
}

interface GetAnimatedEventArgs {
  phase: AnimationPhase;
  restoreProgressPercent: number;
  shedProgressPercent: number;
}

interface StartProgressIntervalArgs {
  onComplete: () => void;
  setProgressPercent: (updater: (currentPercent: number) => number) => void;
}

const animationStepPercent = 10;
const animationStepMs = 450;
const restoreStartDelayMs = 900;
const fanCurtailDelayMs = 1600;
const fanRestoreDelayMs = 1600;

function ActiveCurtailmentStatusStory(props: ComponentProps<typeof ActiveCurtailmentStatus>): ReactElement {
  const [dialogAction, setDialogAction] = useState<CurtailmentStopConfirmationAction>();

  function closeDialog(): void {
    setDialogAction(undefined);
  }

  return (
    <>
      <ActiveCurtailmentStatus
        {...props}
        onDismissRestored={() => undefined}
        onRequestEdit={() => undefined}
        onRequestRestore={() => setDialogAction("restore")}
        onRequestStop={() => setDialogAction("restore")}
      />
      <CurtailmentStopConfirmationDialog
        open={dialogAction !== undefined}
        action={dialogAction ?? "stopCurtailment"}
        onCancel={closeDialog}
        onConfirm={closeDialog}
      />
    </>
  );
}

function startProgressInterval({ onComplete, setProgressPercent }: StartProgressIntervalArgs): number {
  const intervalId = window.setInterval(() => {
    setProgressPercent((currentPercent) => {
      const nextPercent = Math.min(currentPercent + animationStepPercent, 100);

      if (nextPercent === 100) {
        window.clearInterval(intervalId);
        onComplete();
      }

      return nextPercent;
    });
  }, animationStepMs);

  return intervalId;
}

function buildAnimatedEvent({ phase, progressPercent }: BuildAnimatedEventArgs): ActiveCurtailmentEvent {
  const baseEvent: ActiveCurtailmentEvent = {
    ...curtailingCurtailmentEvent,
    facilityFanDeviceCount: 2,
  };
  const targetKw = baseEvent.targetKw ?? baseEvent.estimatedReductionKw;
  const selectedMiners = baseEvent.selectedMiners;
  const completedMiners = Math.round((selectedMiners * progressPercent) / 100);
  const pendingMiners = Math.max(selectedMiners - completedMiners, 0);

  switch (phase) {
    case "restored":
      return {
        ...restoredCurtailmentEvent,
        facilityFanDeviceCount: baseEvent.facilityFanDeviceCount,
        fanOffSentAt: "2026-05-01T12:05:00.000Z",
        fanOnSentAt: "2026-05-01T12:08:00.000Z",
      };
    case "restore":
      return {
        ...baseEvent,
        state: "restoring",
        fanOffSentAt: "2026-05-01T12:05:00.000Z",
        fanOnSentAt: "2026-05-01T12:08:00.000Z",
        observedReductionKw: targetKw,
        rollups: [
          { state: "resolved", count: completedMiners },
          { state: "confirmed", count: pendingMiners },
        ],
      };
    case "shed":
      return {
        ...baseEvent,
        state: "active",
        fanOffSentAt: progressPercent >= 100 ? "2026-05-01T12:05:00.000Z" : undefined,
        observedReductionKw: (targetKw * progressPercent) / 100,
        rollups: [
          { state: "confirmed", count: completedMiners },
          { state: "pending", count: pendingMiners },
        ],
      };
  }
}

function getAnimatedEvent({
  phase,
  restoreProgressPercent,
  shedProgressPercent,
}: GetAnimatedEventArgs): ActiveCurtailmentEvent {
  switch (phase) {
    case "fanRestoreDelay":
      return {
        ...buildAnimatedEvent({ phase: "restore", progressPercent: 0 }),
        fanOnSentAt: "2026-05-01T12:08:00.000Z",
      };
    case "restore":
      return buildAnimatedEvent({ phase: "restore", progressPercent: restoreProgressPercent });
    case "restored":
      return buildAnimatedEvent({ phase: "restored", progressPercent: 100 });
    case "fanCurtailDelay":
      return {
        ...buildAnimatedEvent({ phase: "shed", progressPercent: 100 }),
        fanOffSentAt: undefined,
      };
    case "curtailed":
    case "shed":
      return buildAnimatedEvent({ phase: "shed", progressPercent: shedProgressPercent });
  }
}

function AnimatedCurtailmentLifecycleStory(): ReactElement {
  const [phase, setPhase] = useState<AnimationPhase>("shed");
  const [shedProgressPercent, setShedProgressPercent] = useState(0);
  const [restoreProgressPercent, setRestoreProgressPercent] = useState(0);
  const [dialogAction, setDialogAction] = useState<CurtailmentStopConfirmationAction>();

  useEffect(() => {
    if (phase !== "shed") {
      return;
    }

    const intervalId = startProgressInterval({
      onComplete: () => setPhase("fanCurtailDelay"),
      setProgressPercent: setShedProgressPercent,
    });

    return () => window.clearInterval(intervalId);
  }, [phase]);

  useEffect(() => {
    if (phase !== "fanCurtailDelay") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setPhase("curtailed");
    }, fanCurtailDelayMs);

    return () => window.clearTimeout(timeoutId);
  }, [phase]);

  useEffect(() => {
    if (phase !== "curtailed") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setRestoreProgressPercent(0);
      setPhase("fanRestoreDelay");
    }, restoreStartDelayMs);

    return () => window.clearTimeout(timeoutId);
  }, [phase]);

  useEffect(() => {
    if (phase !== "fanRestoreDelay") {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setRestoreProgressPercent(0);
      setPhase("restore");
    }, fanRestoreDelayMs);

    return () => window.clearTimeout(timeoutId);
  }, [phase]);

  useEffect(() => {
    if (phase !== "restore") {
      return;
    }

    const intervalId = startProgressInterval({
      onComplete: () => setPhase("restored"),
      setProgressPercent: setRestoreProgressPercent,
    });

    return () => window.clearInterval(intervalId);
  }, [phase]);

  const activeEvent = useMemo<ActiveCurtailmentEvent>(
    () => getAnimatedEvent({ phase, restoreProgressPercent, shedProgressPercent }),
    [phase, restoreProgressPercent, shedProgressPercent],
  );

  function resetAnimation(): void {
    setShedProgressPercent(0);
    setRestoreProgressPercent(0);
    setPhase("shed");
  }

  function closeDialog(): void {
    setDialogAction(undefined);
  }

  function confirmRestore(): void {
    closeDialog();
    setRestoreProgressPercent(0);
    setPhase("fanRestoreDelay");
  }

  return (
    <>
      <ActiveCurtailmentStatus
        event={activeEvent}
        onDismissRestored={resetAnimation}
        onRequestEdit={() => undefined}
        onRequestRestore={() => setDialogAction("restore")}
        onRequestStop={() => setDialogAction("restore")}
      />
      <CurtailmentStopConfirmationDialog
        open={dialogAction !== undefined}
        action={dialogAction ?? "stopCurtailment"}
        onCancel={closeDialog}
        onConfirm={confirmRestore}
      />
    </>
  );
}

export const Curtailing: Story = {
  args: {
    event: curtailingCurtailmentEvent,
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const CurtailingWithInfrastructure: Story = {
  name: "Curtailing with infrastructure",
  args: {
    event: {
      ...curtailingCurtailmentEvent,
      facilityFanDeviceCount: 2,
      fanOffSentAt: "2026-05-01T12:05:00.000Z",
    },
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const CurtailingLargeFleet: Story = {
  name: "Curtailing large fleet (progress + ETA)",
  args: {
    event: {
      ...curtailingCurtailmentEvent,
      scopeLabel: "Whole fleet",
      selectedMiners: 5005,
      facilityFanDeviceCount: 10,
      startedAt: new Date(Date.now() - 95_000).toISOString(),
      curtailBatchSize: 500,
      curtailBatchIntervalSec: 30,
      rollups: [
        { state: "confirmed", count: 3000 },
        { state: "dispatched", count: 1000 },
        { state: "drifted", count: 200 },
        { state: "pending", count: 800 },
        { state: "unavailable", count: 5 },
      ],
      unavailableReasonCounts: [
        { label: "offline", count: 3 },
        { label: "needs authentication", count: 1 },
        { label: "in maintenance", count: 1 },
      ],
    },
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const Curtailed: Story = {
  args: {
    event: curtailedCurtailmentEvent,
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const Restoring: Story = {
  args: {
    event: restoringCurtailmentEvent,
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const RestoringWithInfrastructure: Story = {
  name: "Restoring with infrastructure",
  args: {
    event: {
      ...restoringCurtailmentEvent,
      facilityFanDeviceCount: 2,
    },
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const Restored: Story = {
  args: {
    event: restoredCurtailmentEvent,
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const RestoreIncomplete: Story = {
  args: {
    event: restoreIncompleteCurtailmentEvent,
  },
  render: (args) => <ActiveCurtailmentStatusStory {...args} />,
};

export const AnimatedCurtailmentLifecycle: Story = {
  name: "Animated curtailment lifecycle",
  render: function renderAnimatedCurtailmentLifecycle(): ReactElement {
    return <AnimatedCurtailmentLifecycleStory />;
  },
};
