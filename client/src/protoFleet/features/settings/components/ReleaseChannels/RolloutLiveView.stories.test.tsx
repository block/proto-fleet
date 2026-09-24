import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { composeStories } from "@storybook/react-vite";

import { pausedRigRollout } from "./ReleaseChannels.fixtures";
import * as stories from "./RolloutLiveView.stories";

const { Updating, PilotReview, HoldingForReview, Paused } = composeStories(stories, {
  parameters: { actions: { argTypesRegex: "^on[A-Z].*" } },
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it.each([
  { Story: Updating, status: "Updating" },
  { Story: PilotReview, status: "Pilot batch review" },
  { Story: HoldingForReview, status: "Batch review" },
  { Story: Paused, status: "Paused" },
])("renders the $status story with serializable controls and working details", ({ Story, status }) => {
  expect(() => JSON.stringify(Story.args)).not.toThrow();
  render(<Story />);
  expect(screen.getByTestId("inline-rollout-status-headline")).toHaveTextContent(status);
  fireEvent.click(screen.getByRole("button", { name: "View details" }));
  expect(screen.getByTestId("inline-rollout-evidence")).toBeInTheDocument();
});

it("uses an explicit action spy for layout-effect dialog notifications", async () => {
  render(<Paused />);
  expect(Paused.args.onDialogChange).toHaveBeenCalledWith(pausedRigRollout, false);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  expect(screen.queryByTestId("inline-view-rollout-open-action")).not.toBeInTheDocument();
  fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));
  expect(await screen.findByTestId("rollout-miners-modal")).toBeInTheDocument();
  expect(await screen.findByText("Rig A01")).toBeInTheDocument();
  expect(Paused.args.onDialogChange).toHaveBeenLastCalledWith(pausedRigRollout, true);
});
