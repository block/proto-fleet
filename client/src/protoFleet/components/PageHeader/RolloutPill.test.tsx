import { MemoryRouter, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import RolloutPill, { RELEASE_CHANNELS_PATH } from "./RolloutPill";
import {
  type Rollout,
  RolloutBehaviorSchema,
  RolloutDeviceCountsSchema,
  RolloutMethod,
  RolloutSchema,
  RolloutStage,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  activeRigRollout,
  batchedRigRollout,
  gatedRigRollout,
  pausedRigRollout,
  pilotBehavior,
} from "@/protoFleet/features/settings/components/ReleaseChannels/ReleaseChannels.fixtures";

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({ width: 1280, height: 600, isPhone: false, isTablet: false, isDesktop: true }),
}));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const triggerName = "View ongoing firmware updates";
const secondActive = { ...activeRigRollout, id: 101n };
const secondPaused = { ...pausedRigRollout, id: 102n };
const waitingForController = create(RolloutSchema, {
  ...activeRigRollout,
  id: 103n,
  state: RolloutState.WAITING_FOR_CONTROLLER,
  behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.DELEGATED }),
  deviceCounts: create(RolloutDeviceCountsSchema, { done: 2, queued: 4 }),
});
const waitingWithFailures = create(RolloutSchema, {
  ...waitingForController,
  id: 104n,
  deviceCounts: create(RolloutDeviceCountsSchema, { done: 2, queued: 3, failed: 1 }),
});
const pausedWithFailures = {
  ...batchedRigRollout,
  state: RolloutState.PAUSED,
  pausedAt: pausedRigRollout.pausedAt,
};

function CurrentLocation() {
  const location = useLocation();
  return <output>{location.pathname + location.search}</output>;
}

function renderPill(rollouts: Rollout[]) {
  return render(
    <MemoryRouter initialEntries={["/miners"]}>
      <RolloutPill rollouts={rollouts} />
      <button>Next header action</button>
      <CurrentLocation />
    </MemoryRouter>,
  );
}

function mockPopoverGeometry() {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains("rollout-pill-trigger")) return new DOMRect(800, 24, 240, 32);
    if (this.classList.contains("z-50")) return new DOMRect(0, 0, 240, 2000);
    return new DOMRect();
  });
}

describe("RolloutPill", () => {
  it.each([
    { rollouts: [activeRigRollout], label: "Firmware update in progress", pulses: true },
    { rollouts: [activeRigRollout, secondActive], label: "2 firmware updates in progress", pulses: true },
    { rollouts: [pausedRigRollout], label: "Firmware update paused", pulses: false },
    { rollouts: [pausedRigRollout, secondPaused], label: "2 firmware updates paused", pulses: false },
    {
      rollouts: [waitingForController],
      label: "Firmware update waiting for controller",
      pulses: false,
    },
    {
      rollouts: [waitingForController, { ...waitingForController, id: 105n }],
      label: "2 firmware updates waiting for controller",
      pulses: false,
    },
    {
      rollouts: [pausedRigRollout, waitingForController],
      label: "1 firmware update paused, 1 waiting for controller",
      pulses: false,
    },
    {
      rollouts: [activeRigRollout, waitingForController],
      label: "1 firmware update in progress, 1 waiting for controller",
      pulses: true,
    },
    {
      rollouts: [activeRigRollout, pausedRigRollout, waitingForController],
      label: "1 firmware update in progress, 1 paused, 1 waiting for controller",
      pulses: true,
    },
    {
      rollouts: [waitingWithFailures],
      label: "Firmware update needs attention",
      pulses: false,
    },
    {
      rollouts: [activeRigRollout, waitingForController, gatedRigRollout],
      label: "Firmware update needs attention",
      pulses: false,
    },
    {
      rollouts: [
        create(RolloutSchema, {
          ...gatedRigRollout,
          state: RolloutState.STABILIZING_TELEMETRY,
          behavior: create(RolloutBehaviorSchema, {
            ...pilotBehavior,
            autoContinueOnHealthyTelemetry: true,
          }),
        }),
      ],
      label: "Firmware update in progress",
      pulses: true,
    },
    {
      rollouts: [
        create(RolloutSchema, {
          ...waitingForController,
          state: RolloutState.IN_PROGRESS,
          stage: RolloutStage.WAITING,
          behavior: create(RolloutBehaviorSchema, {
            method: RolloutMethod.BATCHED,
            batchSize: 2,
            waitBetweenBatchesSeconds: 60,
          }),
        }),
      ],
      label: "Firmware update in progress",
      pulses: true,
    },
    {
      rollouts: [activeRigRollout, pausedRigRollout, secondPaused],
      label: "1 firmware update in progress, 2 paused",
      pulses: true,
    },
    {
      rollouts: [activeRigRollout, secondActive, pausedRigRollout],
      label: "2 firmware updates in progress, 1 paused",
      pulses: true,
    },
    {
      rollouts: [pausedRigRollout, pausedWithFailures],
      label: "Firmware update needs attention",
      pulses: false,
    },
    {
      rollouts: [activeRigRollout, pausedRigRollout, gatedRigRollout, batchedRigRollout],
      label: "2 firmware updates need attention",
      pulses: false,
    },
  ])("shows $label with an appropriate activity indicator", ({ rollouts, label, pulses }) => {
    renderPill(rollouts);

    const trigger = screen.getByRole("button", { name: triggerName });
    expect(trigger).toHaveTextContent(label);
    expect(trigger.querySelector(".animate-pulse") !== null).toBe(pulses);
  });

  it("updates the indicator when a controller starts work and becomes idle again", () => {
    const view = renderPill([waitingForController]);
    const trigger = screen.getByRole("button", { name: triggerName });
    expect(trigger).toHaveTextContent("Firmware update waiting for controller");
    expect(trigger.querySelector(".animate-pulse")).toBeNull();

    view.rerender(
      <MemoryRouter>
        <RolloutPill
          rollouts={[
            create(RolloutSchema, {
              ...waitingForController,
              state: RolloutState.IN_PROGRESS,
              deviceCounts: create(RolloutDeviceCountsSchema, { done: 2, inProgress: 1, queued: 3 }),
            }),
          ]}
        />
      </MemoryRouter>,
    );
    expect(trigger).toHaveTextContent("Firmware update in progress");
    expect(trigger.querySelector(".animate-pulse")).not.toBeNull();

    view.rerender(
      <MemoryRouter>
        <RolloutPill rollouts={[waitingForController]} />
      </MemoryRouter>,
    );
    expect(trigger).toHaveTextContent("Firmware update waiting for controller");
    expect(trigger.querySelector(".animate-pulse")).toBeNull();
  });

  it.each([waitingForController, waitingWithFailures])(
    "shows the controller wait in rollout $id details",
    (rollout) => {
      renderPill([rollout]);
      fireEvent.click(screen.getByRole("button", { name: triggerName }));

      const entry = screen.getByTestId(`rollout-pill-entry-${rollout.id}`);
      expect(within(entry).getByText("Waiting for controller")).toBeInTheDocument();
    },
  );

  it("distinguishes manufacturers sharing the same channel, model and firmware version", () => {
    const rollouts = [
      { ...activeRigRollout, manufacturer: "Acme", model: "Rig" },
      { ...activeRigRollout, id: 106n, manufacturer: "Other", model: "Rig" },
    ];
    renderPill(rollouts);
    fireEvent.click(screen.getByRole("button", { name: triggerName }));

    for (const rollout of rollouts) {
      const entry = screen.getByTestId(`rollout-pill-entry-${rollout.id}`);
      expect(within(entry).getByText(`${rollout.manufacturer} Rig → ${rollout.firmwareVersion}`)).toBeInTheDocument();
    }
  });

  it("keeps a long desktop list and its navigation link inside a scrollable viewport-constrained surface", async () => {
    const viewport = Object.assign(new EventTarget(), { width: 1280, height: 600 });
    vi.stubGlobal("visualViewport", viewport);
    mockPopoverGeometry();
    const rollouts = Array.from({ length: 40 }, (_, index) => ({
      ...activeRigRollout,
      id: BigInt(index + 1),
      channelName: `Channel ${index + 1}`,
    }));
    renderPill(rollouts);
    const trigger = screen.getByRole("button", { name: triggerName });
    fireEvent.click(trigger);

    const firstEntry = screen.getByTestId("rollout-pill-entry-1");
    const lastEntry = screen.getByTestId("rollout-pill-entry-40");
    const surface = firstEntry.closest<HTMLElement>(".popover-content")!;
    const container = surface.parentElement!;
    expect(container).toHaveClass("fixed");
    expect(container).toHaveStyle({ maxHeight: "584px", visibility: "visible" });
    expect(surface).toHaveClass("max-h-[inherit]", "overflow-y-auto", "overscroll-contain");
    expect(surface).toContainElement(lastEntry);
    expect(trigger.closest(".rollout-pill-trigger")).not.toContainElement(surface);

    // Zoom or browser chrome can shrink the visible viewport without changing
    // the layout viewport. The shared positioner must recalculate the height cap.
    await act(async () => {
      viewport.height = 320;
      viewport.dispatchEvent(new Event("resize"));
    });
    expect(container).toHaveStyle({ maxHeight: "304px" });
    const link = within(surface).getByRole("link", { name: "View release channels" });
    expect(link).toHaveAttribute("href", RELEASE_CHANNELS_PATH);
    fireEvent.click(link);
    expect(screen.getByRole("status")).toHaveTextContent(RELEASE_CHANNELS_PATH);
    expect(screen.queryByTestId("rollout-pill-entry-40")).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });

  it("opens from Enter without scrolling and lets Tab reach the release-channels link", async () => {
    mockPopoverGeometry();
    const user = userEvent.setup();
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    renderPill([activeRigRollout]);
    const trigger = screen.getByRole("button", { name: triggerName });
    trigger.focus();

    await user.keyboard("{Enter}");

    await waitFor(() => expect(trigger).not.toHaveFocus());
    expect(screen.getByRole("region", { name: "Ongoing firmware updates" })).toHaveFocus();
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    await user.tab();
    expect(screen.getByRole("link", { name: "View release channels" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("status")).toHaveTextContent(RELEASE_CHANNELS_PATH);
    expect(screen.queryByRole("link", { name: "View release channels" })).not.toBeInTheDocument();
  });

  it("does not move pointer-opened focus to the content or footer", async () => {
    mockPopoverGeometry();
    const user = userEvent.setup();
    renderPill([activeRigRollout]);

    await user.click(screen.getByRole("button", { name: triggerName }));

    expect(screen.getByRole("region", { name: "Ongoing firmware updates" })).not.toHaveFocus();
    expect(screen.getByRole("link", { name: "View release channels" })).not.toHaveFocus();
  });

  it("returns keyboard focus on Escape without taking focus back after navigation away", async () => {
    mockPopoverGeometry();
    const user = userEvent.setup();
    renderPill([activeRigRollout]);
    const trigger = screen.getByRole("button", { name: triggerName });
    trigger.focus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.getByRole("region", { name: "Ongoing firmware updates" })).toHaveFocus());

    await user.keyboard("{Escape}");

    expect(trigger).toHaveFocus();
    expect(screen.queryByRole("region", { name: "Ongoing firmware updates" })).not.toBeInTheDocument();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.getByRole("region", { name: "Ongoing firmware updates" })).toHaveFocus());
    const nextAction = screen.getByRole("button", { name: "Next header action" });
    act(() => nextAction.focus());
    expect(nextAction).toHaveFocus();
    expect(screen.queryByRole("region", { name: "Ongoing firmware updates" })).not.toBeInTheDocument();
  });
});
