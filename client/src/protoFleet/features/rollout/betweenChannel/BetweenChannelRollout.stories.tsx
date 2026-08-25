import { useMemo, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, within } from "storybook/test";

import AggregateRolloutStatus, { type RolloutChildMutationState } from "./AggregateRolloutStatus";
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
import type { RolloutGroup, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
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
  activity: RolloutGroup["activity"];
  initialChildren: RolloutRecord[];
  terminalOutcome?: RolloutGroup["terminalOutcome"];
  resultReady?: boolean;
  childPresentation?: Record<string, ChildPresentation>;
  phone?: boolean;
}

function aggregateParent(
  name: string,
  activity: RolloutGroup["activity"],
  children: RolloutRecord[],
  terminalOutcome: RolloutGroup["terminalOutcome"] = "pending",
  resultReady = false,
): RolloutGroup {
  const terminal = terminalOutcome !== "pending";
  return {
    id: "aggregate-story-parent",
    laneId: stableProductionLane.id,
    name,
    reason: "Deterministic Storybook fixture",
    resultRevision: terminal ? 1n : 0n,
    terminalOutcome,
    resultReady,
    lifecycle: terminal ? "terminal" : "active",
    activity,
    needsAction: activity === "attentionRequired",
    evidenceReadiness: resultReady ? "ready" : "pending",
    models: children.map((child) => ({
      laneModelId: child.laneModelId ?? `declaration-${child.id}`,
      modelIdentityKey: child.modelIdentityKey ?? `story:${child.id}`,
      manufacturer: child.manufacturer ?? "",
      model: child.model ?? "",
      sourceChannelId: child.sourceChannelId ?? 41n,
      targetChannelId: child.targetChannelId ?? 42n,
      sourceReleaseTargetId: 1n,
      targetReleaseTargetId: 2n,
      memberCount: child.members.length,
      childRolloutId: child.id,
    })),
    children,
  };
}

function AggregateRolloutStory({
  name,
  activity,
  initialChildren,
  terminalOutcome,
  resultReady = false,
  childPresentation = {},
  phone = false,
}: AggregateRolloutStoryProps) {
  const [children, setChildren] = useState(initialChildren);
  const [focusedChildId, setFocusedChildId] = useState<string | null>(initialChildren[0]?.id ?? null);
  const [childMutationState, setChildMutationState] = useState<Record<string, RolloutChildMutationState>>(() =>
    Object.fromEntries(
      Object.entries(childPresentation).map(([childId, presentation]) => [
        childId,
        presentation === "loading" ? { loading: true } : { loading: false, error: presentation.error },
      ]),
    ),
  );
  const parent = useMemo(
    () => aggregateParent(name, activity, children, terminalOutcome, resultReady),
    [activity, children, name, resultReady, terminalOutcome],
  );
  const updateChild = (child: RolloutRecord, state: RolloutRecord["state"]) => {
    setChildren((current) =>
      current.map((candidate) =>
        candidate.id === child.id ? { ...candidate, state, revision: candidate.revision + 1n } : candidate,
      ),
    );
  };

  return (
    <AppShell>
      <main className="grid gap-6 p-6 tablet:p-10">
        <Header
          title="Firmware"
          titleSize="text-heading-300"
          description="Manage stable firmware lanes and model rollouts."
        />
        <div className={phone ? "mx-auto w-full max-w-[390px]" : "w-full"}>
          <AggregateRolloutStatus
            parent={parent}
            children={children}
            focusedChildId={focusedChildId}
            laneLabel={stableProductionLane.label}
            canControl
            childMutationState={childMutationState}
            onFocusChange={setFocusedChildId}
            onAdmit={(child) => {
              setChildMutationState((current) => ({ ...current, [child.id]: { loading: false } }));
              updateChild(child, "running");
            }}
            onPause={(child) => updateChild(child, "paused")}
            onResume={(child) => updateChild(child, "running")}
            onContinue={(child) => updateChild(child, "running")}
            onAbort={(child) => updateChild(child, "aborted")}
            onRevert={(child) => updateChild(child, "reverting")}
            onCompleteWithFailures={(child) => updateChild(child, "completedWithFailures")}
          />
        </div>
      </main>
    </AppShell>
  );
}

export const OneModelPartialRollout: Story = {
  name: "One-model partial rollout",
  render: () => (
    <AggregateRolloutStory name="Proto Alpha 2.0" activity="running" initialChildren={[protoAlphaRunningChild]} />
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const trigger = canvas.getByRole("button", { name: /Proto Alpha.*running/ });
    const panelId = trigger.getAttribute("aria-controls");
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(panelId).toBe("rollout-child-proto-alpha-child");
    expect(canvasElement.querySelector(`#${panelId}`)).toBeInTheDocument();

    await userEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(canvasElement.querySelector(`#${panelId}`)).not.toBeInTheDocument();
    await userEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
  },
};

export const TwoModelsInDifferentActiveStates: Story = {
  name: "Two models in different active states",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="review"
      initialChildren={[protoAlphaReviewChild, antminerS21RunningChild]}
    />
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const liveRegions = canvas.getAllByRole("status");
    expect(liveRegions).toHaveLength(1);
    expect(liveRegions[0]).toHaveAttribute("aria-live", "polite");

    await userEvent.click(canvas.getByRole("button", { name: "Continue" }));
    expect(canvas.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
    await userEvent.click(canvas.getByRole("button", { name: /Antminer S21.*running/ }));
    await userEvent.click(canvas.getByRole("button", { name: "Pause" }));
    expect(canvas.getByRole("button", { name: /Antminer S21.*paused/ })).toBeInTheDocument();
    expect(canvas.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
  },
};

export const MixedTerminalResult: Story = {
  name: "Mixed terminal result",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="settled"
      terminalOutcome="mixed"
      resultReady
      initialChildren={[protoAlphaSuccessfulChild, antminerS21AbortedChild]}
    />
  ),
};

export const SplitModelNeedsAttention: Story = {
  name: "Split model needs attention",
  render: () => (
    <AggregateRolloutStory
      name="August model updates"
      activity="attentionRequired"
      terminalOutcome="completedWithFailures"
      resultReady
      initialChildren={[protoAlphaSplitChild, antminerS21AbortedChild]}
    />
  ),
};

export const ChildLocalErrorAndLoading: Story = {
  name: "Child-local error and loading",
  render: () => (
    <AggregateRolloutStory
      name="Independent model admission"
      activity="created"
      initialChildren={[protoAlphaRunningChild, antminerS21CreatedChild]}
      childPresentation={{
        [protoAlphaRunningChild.id]: "loading",
        [antminerS21CreatedChild.id]: { error: "The model start response could not be confirmed." },
      }}
    />
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const sibling = canvas.getByRole("button", { name: /Proto Alpha.*running/ });
    await userEvent.click(canvas.getByRole("button", { name: /Antminer S21.*created/ }));
    await userEvent.click(canvas.getByRole("button", { name: "Retry model start" }));

    expect(canvas.queryByText("The model start response could not be confirmed.")).not.toBeInTheDocument();
    expect(canvas.getByRole("button", { name: /Antminer S21.*running/ })).toBeInTheDocument();
    expect(sibling).toBeInTheDocument();
    expect(canvas.getByRole("button", { name: /Proto Alpha.*running/ })).toBeInTheDocument();
  },
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
      initialChildren={[protoAlphaReviewChild, antminerS21AbortedChild]}
      phone
    />
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    expect(canvas.getByRole("button", { name: "Continue" })).toBeInTheDocument();
    expect(canvas.queryByRole("button", { name: "Abort rollout" })).not.toBeInTheDocument();

    await userEvent.click(canvas.getByRole("button", { name: /More actions for Proto Alpha rollout/ }));
    await userEvent.click(within(document.body).getByText("Abort rollout"));
    expect(within(document.body).getByText("Abort Proto Alpha rollout for 3 miners?")).toBeInTheDocument();
    await userEvent.click(within(document.body).getByRole("button", { name: "Cancel" }));

    await userEvent.click(canvas.getByRole("button", { name: /Antminer S21.*aborted/ }));
    expect(canvas.queryByRole("button", { name: "Revert" })).not.toBeInTheDocument();
    await userEvent.click(canvas.getByRole("button", { name: /More actions for Antminer S21 rollout/ }));
    await userEvent.click(
      within(within(document.body).getByTestId("active-rollout-more-actions-menu")).getByText("Revert"),
    );
    expect(within(document.body).getByText("Revert Antminer S21 for 1 confirmed miner?")).toBeInTheDocument();
  },
};
