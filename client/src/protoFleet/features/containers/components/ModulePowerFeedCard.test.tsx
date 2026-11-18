import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import ModulePowerFeedCard from "./ModulePowerFeedCard";
import {
  GetModulePowerFeedResponseSchema,
  PowerFeedAction,
  PowerFeedMappingConfidence,
  PowerFeedState,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const client = vi.hoisted(() => ({ getModulePowerFeed: vi.fn(), controlModulePowerFeed: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: client }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useAuthErrors: () => ({ handleAuthErrors: ({ onError }: { onError: () => void }) => onError() }),
}));

beforeEach(() => {
  vi.useRealTimers();
  client.getModulePowerFeed.mockReset();
  client.controlModulePowerFeed.mockReset();
});

const controllable = (unavailableReason = "") =>
  create(GetModulePowerFeedResponseSchema, {
    moduleId: 91n,
    deviceIdentifier: "north",
    containerName: "North CT1",
    tank: 1,
    module: 2,
    mapping: { confidence: PowerFeedMappingConfidence.CONFIGURED, feedId: "pdu-1-outlet-05" },
    feed: { id: "pdu-1-outlet-05", state: PowerFeedState.ENABLED, faulted: false },
    recoveryCycleKnown: true,
    control: { actions: [PowerFeedAction.TURN_OFF, PowerFeedAction.CYCLE], unavailableReason, cycleHoldSeconds: 30 },
  });

it("reads the module's feed once, refreshes on request and drops the reading when a refresh fails", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  client.getModulePowerFeed.mockResolvedValueOnce(
    create(GetModulePowerFeedResponseSchema, {
      observedAt: timestampFromMs(Date.UTC(2026, 10, 15, 15, 4, 5)),
      mapping: { confidence: PowerFeedMappingConfidence.WIRING_VERIFIED, feedId: "pdu-1-outlet-05" },
      feed: { id: "pdu-1-outlet-05", state: PowerFeedState.ENABLED, voltageV: 480 },
      recoveryCycleKnown: true,
    }),
  );
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  expect(screen.getByRole("status").textContent).toBe("Loading power feed…");
  expect(await screen.findByText("pdu-1-outlet-05")).toBeTruthy();
  expect(screen.getByText("Wiring verified")).toBeTruthy();
  expect(screen.getByText(/^Read /).textContent).toBe(
    `Read ${formatTimestamp(Date.UTC(2026, 10, 15, 15, 4, 5) / 1000, { includeSeconds: true })}`,
  );
  expect(client.getModulePowerFeed).toHaveBeenCalledExactlyOnceWith(
    { moduleId: 91n },
    expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 20_000 }),
  );

  // Never polled: each read holds the controller's binding lock.
  await act(() => vi.advanceTimersByTimeAsync(60_000));
  expect(client.getModulePowerFeed).toHaveBeenCalledOnce();
  vi.useRealTimers();

  let fail: (cause: Error) => void = () => {};
  client.getModulePowerFeed.mockReturnValueOnce(new Promise((_, reject) => (fail = reject)));
  fireEvent.click(screen.getByRole("button", { name: "Refresh power feed" }));
  const refreshing = screen.getByRole("button", { name: "Refresh power feed" });
  expect(refreshing.textContent).toBe("Refreshing…");
  expect(refreshing).toHaveProperty("disabled", true);
  await act(async () => fail(new Error("controller unreachable")));
  expect((await screen.findByRole("alert")).textContent).toContain("controller unreachable");
  expect(screen.queryByText("pdu-1-outlet-05")).toBeNull();
  expect(client.getModulePowerFeed).toHaveBeenCalledTimes(2);
});

it("shows the server's reason when the read is refused and retries on request", async () => {
  client.getModulePowerFeed.mockRejectedValueOnce(
    new Error("power feed inspection is not supported by this controller"),
  );
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  expect((await screen.findByRole("alert")).textContent).toContain("not supported by this controller");
  client.getModulePowerFeed.mockResolvedValueOnce(
    create(GetModulePowerFeedResponseSchema, {
      mapping: { confidence: PowerFeedMappingConfidence.UNAVAILABLE, reasons: ["no_feed_assigned"] },
      recoveryCycleKnown: true,
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry power feed" }));
  expect(await screen.findByText("No feed is assigned to this module.")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("does not read while disabled", () => {
  render(<ModulePowerFeedCard moduleId={91n} enabled={false} />);
  expect(client.getModulePowerFeed).not.toHaveBeenCalled();
  expect(screen.queryByRole("status")).toBeNull();
});

it("offers no feed controls without control access", async () => {
  client.getModulePowerFeed.mockResolvedValue(
    create(GetModulePowerFeedResponseSchema, {
      mapping: { confidence: PowerFeedMappingConfidence.CONFIGURED, feedId: "pdu-1-outlet-05" },
      feed: { id: "pdu-1-outlet-05", state: PowerFeedState.ENABLED },
      recoveryCycleKnown: true,
    }),
  );
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  expect(await screen.findByText("pdu-1-outlet-05")).toBeTruthy();
  expect(screen.queryByTestId("power-feed-actions-trigger")).toBeNull();
});

it("explains why the offered controls cannot run now", async () => {
  client.getModulePowerFeed.mockResolvedValue(controllable("Another power feed command is pending for this module."));
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  fireEvent.click(await screen.findByTestId("power-feed-actions-trigger"));
  const turnOff = screen.getByText("Turn off feed").closest("button")!;
  expect(turnOff).toHaveProperty("disabled", true);
  expect(screen.getAllByText("Another power feed command is pending for this module.").length).toBeGreaterThan(0);
  fireEvent.click(turnOff);
  expect(screen.queryByTestId("power-feed-action-dialog")).toBeNull();
});

it("says why no control is offered when the server sends no actions", async () => {
  const response = controllable("Controls are off because the feed's state is unknown.");
  response.control!.actions = [];
  client.getModulePowerFeed.mockResolvedValue(response);
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  fireEvent.click(await screen.findByTestId("power-feed-actions-trigger"));
  expect(screen.getAllByText("Controls are off because the feed's state is unknown.").length).toBeGreaterThan(0);
  expect(screen.queryByText("Turn off feed")).toBeNull();
});

it("confirms the chosen action against the feed it was offered for", async () => {
  client.getModulePowerFeed.mockResolvedValue(controllable());
  render(<ModulePowerFeedCard moduleId={91n} enabled />);
  fireEvent.click(await screen.findByTestId("power-feed-actions-trigger"));
  fireEvent.click(screen.getByText("Cycle feed"));
  const dialog = screen.getByTestId("power-feed-action-dialog");
  expect(dialog.textContent).toContain("Cycle feed pdu-1-outlet-05?");
  expect(dialog.textContent).toContain("North CT1 · Tank 1 · Module 2");
  expect(dialog.textContent).toContain("after 30 seconds");
  expect(client.controlModulePowerFeed).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByTestId("power-feed-action-dialog")).toBeNull();
});
