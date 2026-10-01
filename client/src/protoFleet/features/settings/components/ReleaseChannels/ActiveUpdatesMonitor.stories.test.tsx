import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { composeStories } from "@storybook/react-vite";

import * as stories from "./ActiveUpdatesMonitor.stories";

const { SingleInProgress, WaitingForTelemetry, Paused } = composeStories(stories);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("uses the production page navigation and scroll target for Manage", () => {
  const scrollIntoView = vi.fn();
  const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView });
  try {
    render(<SingleInProgress />);
    fireEvent.click(screen.getByTestId("inline-view-rollout-manage-action"));
    const navigation = screen.getByTestId("firmware-tab-navigation");
    expect(within(navigation).getByRole("button", { name: "Release channels" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(scrollIntoView).toHaveBeenCalledExactlyOnceWith({ block: "start", behavior: "instant" });
    expect(scrollIntoView.mock.contexts[0]).toBe(navigation);
    expect(screen.getByTestId("channel-row-Canary")).toBeInTheDocument();
  } finally {
    if (originalScrollIntoView) {
      Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScrollIntoView);
    } else {
      Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
    }
  }
});

it("opens the production miners dialog from a paused update", async () => {
  render(<Paused />);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  fireEvent.click(screen.getByTestId("inline-view-rollout-view-miners-action"));
  expect(await screen.findByTestId("rollout-miners-modal")).toBeInTheDocument();
  expect(await screen.findByText("Rig A01")).toBeInTheDocument();
});

it("uses the monitor's retry confirmation for the paused snapshot", () => {
  render(<Paused />);
  fireEvent.click(screen.getByTestId("inline-view-rollout-more-actions-trigger"));
  fireEvent.click(screen.getByTestId("inline-view-rollout-retry-action"));
  expect(screen.getByTestId("retry-rollout-dialog")).toHaveTextContent("This does not advance review gates.");
  expect(screen.getByTestId("confirm-rollout-retry")).toBeEnabled();
});

it("keeps the telemetry hold visible without exposing bigint fixtures to controls", () => {
  expect(() => JSON.stringify(WaitingForTelemetry.args)).not.toThrow();
  render(<WaitingForTelemetry />);
  expect(screen.getByTestId("inline-rollout-status-headline")).toHaveTextContent("Waiting for telemetry");
  expect(screen.getByText("Continues automatically in 2m if telemetry holds.")).toBeInTheDocument();
});
