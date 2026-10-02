import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  gatedRigRollout,
  pausedRigRollout,
} from "./ReleaseChannels.fixtures";
import RolloutProgressIndicator from "./RolloutProgressIndicator";
import {
  RolloutDeviceCountsSchema,
  RolloutStage,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

afterEach(cleanup);

const group = canaryChannel.modelGroups[0];
const trigger = () => screen.getByRole("button", { name: "Show update details for Proto Rig" });

describe("rollout progress bar", () => {
  it.each([
    { done: 0, queued: 6, percent: 0, summary: "0 of 6 miners updated (0%)" },
    { done: 2, queued: 4, percent: 33, summary: "2 of 6 miners updated (33%)" },
    { done: 6, queued: 0, percent: 100, summary: "6 miners updated (100%)" },
    { done: 199, queued: 1, percent: 99, summary: "199 of 200 miners updated (99%)" },
  ])("announces $summary and exposes the matching progress", ({ done, queued, percent, summary }) => {
    render(
      <RolloutProgressIndicator
        group={group}
        rollout={{ ...activeRigRollout, deviceCounts: create(RolloutDeviceCountsSchema, { done, queued }) }}
        testId="progress"
      />,
    );

    const progress = screen.getByRole("progressbar", { name: /Updating to 1\.4\.4$/ });
    expect(progress).toHaveAttribute("data-testid", "progress");
    expect(progress).toHaveAttribute("aria-valuemin", "0");
    expect(progress).toHaveAttribute("aria-valuemax", "100");
    expect(progress).toHaveAttribute("aria-valuenow", `${percent}`);
    expect(progress).toHaveAttribute("aria-valuetext", expect.stringContaining(`Overall: ${summary}`));
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("keeps unknown and empty counts at zero", () => {
    const { rerender } = render(
      <RolloutProgressIndicator group={group} rollout={{ ...activeRigRollout, deviceCounts: undefined }} />,
    );
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "0");
    expect(screen.getByRole("progressbar")).toHaveAttribute(
      "aria-valuetext",
      expect.stringContaining("0 of 0 miners updated (0%)"),
    );
    rerender(
      <RolloutProgressIndicator
        group={group}
        rollout={{ ...activeRigRollout, deviceCounts: create(RolloutDeviceCountsSchema, { skipped: 2, excluded: 1 }) }}
      />,
    );
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "0");
  });

  it("keeps the bar on overall progress and distinguishes current-batch status", async () => {
    const user = userEvent.setup();
    render(
      <RolloutProgressIndicator
        group={group}
        rollout={{
          ...batchedRigRollout,
          state: RolloutState.IN_PROGRESS,
          stage: RolloutStage.BATCH,
          deviceCounts: create(RolloutDeviceCountsSchema, { done: 4, queued: 2 }),
          currentBatchCounts: create(RolloutDeviceCountsSchema, { done: 1, queued: 1 }),
        }}
      />,
    );
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "67");
    await user.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent("Current batch: Batch 2 of 3: updating 1 of 2");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Overall: 4 of 6 miners updated (67%)");
  });

  it("keeps skipped and excluded miners neutral and out of the denominator", async () => {
    const user = userEvent.setup();
    render(
      <RolloutProgressIndicator
        group={group}
        rollout={{
          ...activeRigRollout,
          deviceCounts: create(RolloutDeviceCountsSchema, { done: 3, skipped: 2, excluded: 1 }),
        }}
      />,
    );
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
    expect(screen.queryByTestId("rollout-attention-indicator")).not.toBeInTheDocument();
    await user.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent("Overall: 3 miners updated (100%)");
    expect(screen.getByRole("tooltip")).toHaveTextContent("2 skipped, 1 excluded (not included in progress)");
  });

  it.each([
    { name: "failed", rollout: batchedRigRollout, label: /1 failed/, cue: "rollout-attention-indicator" },
    { name: "manual review", rollout: gatedRigRollout, label: /Review needed/, cue: "rollout-attention-indicator" },
    { name: "paused", rollout: pausedRigRollout, label: /Paused, 2 of 6/, cue: "rollout-paused-indicator" },
  ])("retains the $name status and visible cue", async ({ rollout, label, cue }) => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={rollout} />);
    expect(screen.getByRole("progressbar", { name: label })).toBeInTheDocument();
    expect(screen.getByTestId(cue)).toBeInTheDocument();
    await user.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent(label);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Updating to 1.4.4");
  });

  it("retains an unavailable assigned firmware warning during an active update", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={{ ...group, firmwareAvailable: false }} rollout={activeRigRollout} />);
    expect(screen.getByRole("progressbar", { name: /Assigned firmware unavailable/ })).toBeInTheDocument();
    expect(screen.getByTestId("rollout-attention-indicator")).toBeInTheDocument();
    await user.hover(trigger());
    expect(screen.getByRole("tooltip")).toHaveTextContent("Assigned firmware unavailable");
  });
});

describe("rollout progress tooltip", () => {
  it("opens on hover, remains hoverable, and closes after leaving", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={activeRigRollout} />);
    await user.hover(trigger());
    const tooltip = screen.getByRole("tooltip");
    // user-event v14 omits relatedTarget when moving between elements.
    fireEvent.mouseOut(trigger(), { relatedTarget: tooltip });
    fireEvent.mouseOver(tooltip, { relatedTarget: trigger() });
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.mouseOut(tooltip, { relatedTarget: document.body });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("opens on keyboard focus, dismisses with Escape, and reopens after refocusing", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={activeRigRollout} />);
    await user.tab();
    expect(trigger()).toHaveFocus();
    expect(trigger()).toHaveAttribute("aria-describedby", screen.getByRole("tooltip").id);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(trigger()).toHaveFocus();
    await user.tab();
    await user.tab();
    expect(screen.getByRole("tooltip")).toBeVisible();
    await user.tab();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("allows tapping to keep details open and tapping again to dismiss", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={activeRigRollout} />);
    await user.pointer([{ keys: "[TouchA>]", target: trigger() }, { keys: "[/TouchA]" }]);
    expect(screen.getByRole("tooltip")).toBeVisible();
    await user.pointer([{ keys: "[TouchA>]", target: trigger() }, { keys: "[/TouchA]" }]);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("dismisses a tooltip opened by hover with Escape without requiring focus", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={activeRigRollout} />);
    await user.hover(trigger());
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("dismisses pinned details when tapping outside on a nonfocusable element", async () => {
    const user = userEvent.setup();
    render(<RolloutProgressIndicator group={group} rollout={activeRigRollout} />);
    await user.pointer([{ keys: "[TouchA>]", target: trigger() }, { keys: "[/TouchA]" }]);
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.touchStart(document.body);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
});
