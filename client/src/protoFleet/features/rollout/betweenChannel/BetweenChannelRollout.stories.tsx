import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  abortedRollout,
  antminerS21AbortedChild,
  antminerS21CreatedChild,
  antminerS21RunningChild,
  attentionRequiredRollout,
  betweenChannelFiles,
  protoAlphaReviewChild,
  protoAlphaRunningChild,
  protoAlphaSplitChild,
  protoAlphaSuccessfulChild,
  stableProductionLane,
} from "./betweenChannel.fixtures";
import BetweenChannelRolloutStatus from "./BetweenChannelRolloutStatus";
import StartRolloutLaneModal from "./StartRolloutLaneModal";
import { AppShell } from "@/protoFleet/features/rollout/activeRolloutStoryHelpers";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import Header from "@/shared/components/Header";

const noop = (): void => undefined;

const meta = {
  title: "Proto Fleet/Rollout/Between Channel",
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 tablet:p-10">
        <Story />
      </div>
    ),
  ],
} satisfies Meta;

export default meta;
type Story = StoryObj;

export const AttentionRequired: Story = {
  render: () => (
    <BetweenChannelRolloutStatus
      rollout={attentionRequiredRollout}
      laneLabel={stableProductionLane.label}
      canControl
      onPause={noop}
      onAbort={noop}
    />
  ),
};

export const AbortedAndRevertEligible: Story = {
  render: () => (
    <BetweenChannelRolloutStatus
      rollout={abortedRollout}
      laneLabel={stableProductionLane.label}
      canControl
      onRevert={noop}
    />
  ),
};

export const StartFlow: Story = {
  args: {
    rollout: attentionRequiredRollout,
    laneLabel: stableProductionLane.label,
    canControl: true,
  },
  render: () => (
    <StartRolloutLaneModal
      open
      lane={stableProductionLane}
      files={betweenChannelFiles}
      isSubmitting={false}
      onDismiss={noop}
      onStart={noop}
    />
  ),
};

type ChildPresentation = "loading" | { error: string };

interface AggregateRolloutStoryProps {
  name: string;
  activity: string;
  children: RolloutRecord[];
  terminalOutcome?: string;
  resultReady?: boolean;
  childPresentation?: Record<string, ChildPresentation>;
  phone?: boolean;
}

function AggregateRolloutStory({
  name,
  activity,
  children,
  terminalOutcome,
  resultReady = false,
  childPresentation = {},
  phone = false,
}: AggregateRolloutStoryProps) {
  const [expandedChildId, setExpandedChildId] = useState<string | null>(children[0]?.id ?? null);

  return (
    <AppShell>
      <main className="grid gap-6 p-6 tablet:p-10">
        <Header
          title="Firmware"
          titleSize="text-heading-300"
          description="Manage stable firmware lanes and model rollouts."
        />
        <div className={phone ? "mx-auto w-full max-w-[390px]" : "w-full"}>
          <section className="grid gap-4" aria-label={`Aggregate rollout ${name}`}>
            <div className="grid gap-2 rounded-2xl border border-border-5 bg-surface-overlay p-5">
              <div className="text-200 text-text-primary-50">Overall rollout</div>
              <div className="text-heading-200 text-text-primary">{name}</div>
              <div className="text-300 text-text-primary-70">
                {children.length} selected model{children.length === 1 ? "" : "s"} · {activity}
              </div>
              <div className="text-200 text-text-primary-70">Controls are available on each model rollout below.</div>
              {terminalOutcome ? (
                <div className="border-t border-border-5 pt-3">
                  <div className="text-emphasis-300 text-text-primary">Result: {terminalOutcome}</div>
                  <div className="text-200 text-text-primary-70">
                    {resultReady ? "Result ready" : "Waiting for final evidence"}
                  </div>
                </div>
              ) : null}
            </div>

            {children.map((child) => {
              const modelLabel = `${child.manufacturer} ${child.model}`;
              const expanded = expandedChildId === child.id;
              const panelId = `story-child-${child.id}`;
              const presentation = childPresentation[child.id];
              return (
                <section
                  key={child.id}
                  className="grid gap-3 rounded-2xl border border-border-5 bg-surface-base p-4 phone:p-3"
                  aria-label={`${modelLabel} rollout`}
                >
                  <button
                    type="button"
                    className="flex min-h-11 items-center justify-between gap-3 text-left"
                    aria-expanded={expanded}
                    aria-controls={panelId}
                    onClick={() => setExpandedChildId(expanded ? null : child.id)}
                  >
                    <span>
                      <span className="block text-emphasis-300 text-text-primary">{modelLabel}</span>
                      <span className="block text-200 text-text-primary-70">
                        {child.state.replace(/([A-Z])/g, " $1").toLowerCase()} · {child.members.length} miners
                      </span>
                    </span>
                    <span aria-hidden>{expanded ? "−" : "+"}</span>
                  </button>
                  {presentation === "loading" ? (
                    <div className="text-200 text-text-primary-70" role="status">
                      Loading {modelLabel} rollout...
                    </div>
                  ) : presentation ? (
                    <div className="rounded-xl border border-intent-critical-fill p-4 text-300 text-text-primary">
                      <div className="text-emphasis-300">{modelLabel} needs attention</div>
                      <div>{presentation.error}</div>
                      <button type="button" className="mt-2 underline">
                        Retry {modelLabel}
                      </button>
                    </div>
                  ) : null}
                  {expanded && presentation !== "loading" ? (
                    <div id={panelId}>
                      <BetweenChannelRolloutStatus
                        rollout={child}
                        laneLabel={stableProductionLane.label}
                        canControl
                        isMutating={false}
                        onAdmit={noop}
                        onPause={noop}
                        onResume={noop}
                        onContinue={noop}
                        onAbort={noop}
                        onRevert={noop}
                        onCompleteWithFailures={noop}
                      />
                    </div>
                  ) : null}
                </section>
              );
            })}
          </section>
        </div>
      </main>
    </AppShell>
  );
}

export const OneModelPartialRollout: Story = {
  name: "One-model partial rollout",
  render: () => <AggregateRolloutStory name="Proto Alpha 2.0" activity="running" children={[protoAlphaRunningChild]} />,
};

export const TwoModelsInDifferentActiveStates: Story = {
  name: "Two models in different active states",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="review"
      children={[protoAlphaReviewChild, antminerS21RunningChild]}
    />
  ),
};

export const MixedTerminalResult: Story = {
  name: "Mixed terminal result",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="settled"
      terminalOutcome="mixed"
      resultReady
      children={[protoAlphaSuccessfulChild, antminerS21AbortedChild]}
    />
  ),
};

export const SplitModelNeedsAttention: Story = {
  name: "Split model needs attention",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="attention required"
      terminalOutcome="completed with failures"
      resultReady
      children={[protoAlphaSplitChild, antminerS21AbortedChild]}
    />
  ),
};

export const ChildLocalErrorAndLoading: Story = {
  name: "Child-local error and loading",
  render: () => (
    <AggregateRolloutStory
      name="Independent model admission"
      activity="created"
      children={[protoAlphaRunningChild, antminerS21CreatedChild]}
      childPresentation={{
        [protoAlphaRunningChild.id]: "loading",
        [antminerS21CreatedChild.id]: { error: "The model start response could not be confirmed." },
      }}
    />
  ),
};

export const PhoneLayout: Story = {
  name: "Phone layout",
  parameters: {
    viewport: { defaultViewport: "mobile1" },
  },
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="review"
      children={[protoAlphaReviewChild, antminerS21RunningChild]}
      phone
    />
  ),
};
