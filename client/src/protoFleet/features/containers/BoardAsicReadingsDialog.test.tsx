import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import BoardAsicReadingsDialog from "./BoardAsicReadingsDialog";
import { boardAsicFrame, controllableBoardAsicStream } from "./boardAsicStreamFixtures";
import HashboardPoolAction from "./HashboardPoolAction";
import { BOARD_ASIC_STREAM_RETRY_MS } from "./useBoardAsicStream";
import { containersClient } from "@/protoFleet/api/clients";
import { HashboardSlotSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const mocks = vi.hoisted(() => ({ permissions: [] as string[] }));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const board = { hashboardId: 101n, installationId: 1010n };
const start = Date.UTC(2026, 10, 14, 8, 0);
const flush = () => act(async () => {});

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date"] });
  vi.setSystemTime(start);
});
afterEach(() => {
  vi.useRealTimers();
});

const renderDialog = () => {
  const stream = controllableBoardAsicStream();
  const onClose = vi.fn();
  const view = render(
    <BoardAsicReadingsDialog board={board} position={3} onClose={onClose} openStream={stream.open} />,
  );
  return { ...stream, onClose, view };
};

it("shows the board's live readings and stops streaming when closed", async () => {
  const { opened, view } = renderDialog();
  expect(screen.getByRole("status")).toHaveTextContent("Connecting to the controller");
  await act(async () => opened[0].push(boardAsicFrame({ temperature: (i) => (i === 5 ? 91 : 60), quarantined: [5] })));

  expect(screen.getByRole("status")).toHaveTextContent("Live.");
  expect(screen.getByText("312")).toBeInTheDocument();
  expect(screen.getByText("91.0 °C")).toBeInTheDocument();
  expect(screen.getByText("140.4 TH/s")).toBeInTheDocument();
  // Each 52-ASIC stack arrives as one long row and wraps to the board's 26 columns.
  expect(document.querySelector('[data-testid="asic-11-25"]')).not.toBeNull();
  expect(document.querySelector('[data-testid="asic-0-5"]')).toHaveAttribute("data-value", "91");
  expect(document.querySelector('[data-testid="asic-12-0"]')).toBeNull();
  expect(document.querySelector('[data-testid="asic-0-26"]')).toBeNull();

  view.unmount();
  expect(opened[0].signal.aborted).toBe(true);
});

it("reconnects at once when the server ends a stream after readings", async () => {
  const { opened } = renderDialog();
  await act(async () => opened[0].push(boardAsicFrame()));
  await act(async () => opened[0].end());
  expect(opened).toHaveLength(2);
  expect(screen.getByRole("status")).toHaveTextContent("Live.");
  expect(screen.queryByText(/Reconnecting/)).toBeNull();
});

it("keeps the last readings marked not live while it reconnects after a failure", async () => {
  const { opened } = renderDialog();
  await act(async () => opened[0].push(boardAsicFrame()));
  await act(async () =>
    opened[0].fail(new ConnectError("the controller stopped sending ASIC readings", Code.Unavailable)),
  );

  expect(screen.getByText("Reconnecting to live ASIC readings")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Not live. Last reading at");
  expect(screen.getByTestId("board-asic-readings-current")).toHaveClass("opacity-50");
  await act(async () => vi.advanceTimersByTime(BOARD_ASIC_STREAM_RETRY_MS));
  expect(opened).toHaveLength(2);
  await act(async () => opened[1].push(boardAsicFrame({ observedAt: start + 5_000 })));
  expect(screen.queryByText("Reconnecting to live ASIC readings")).toBeNull();
});

it("marks readings not live when frames stop arriving", async () => {
  const { opened } = renderDialog();
  await act(async () => opened[0].push(boardAsicFrame()));
  await act(async () => vi.advanceTimersByTime(11_000));
  expect(screen.getByRole("status")).toHaveTextContent("Not live.");
});

it("marks readings not live when the controller keeps resending one sample", async () => {
  const { opened } = renderDialog();
  for (let second = 0; second < 12; second += 1) {
    await act(async () => opened[0].push(boardAsicFrame({ observedAt: start })));
    await act(async () => vi.advanceTimersByTime(1_000));
  }
  expect(screen.getByRole("status")).toHaveTextContent("Not live.");
  await act(async () => opened[0].push(boardAsicFrame({ observedAt: start + 12_000 })));
  expect(screen.getByRole("status")).toHaveTextContent("Live.");
});

it("leaves out ASICs placed outside the grid and keeps the rest", async () => {
  const { opened } = renderDialog();
  const frame = boardAsicFrame();
  frame.asics[0].position = {
    stackId: 0,
    stackIndex: 26,
    x: 26,
    y: 2_147_483_647,
    $typeName: frame.asics[0].position!.$typeName,
  };
  await act(async () => opened[0].push(frame));
  expect(screen.getByText("312")).toBeInTheDocument();
  expect(document.querySelector('[data-testid="asic-11-25"]')).not.toBeNull();
  expect(document.querySelectorAll('[data-testid^="asic-"]')).toHaveLength(12 * 26);
});

it("stops without retrying when the board cannot stream", async () => {
  const { opened } = renderDialog();
  await act(async () =>
    opened[0].fail(
      new ConnectError(
        "Live ASIC readings need a newer Fleet Node. Update the Fleet Node that reaches this container, then reopen the board.",
        Code.FailedPrecondition,
      ),
    ),
  );
  expect(screen.getByText("Live ASIC readings stopped")).toBeInTheDocument();
  expect(screen.getByText(/need a newer Fleet Node/)).toBeInTheDocument();
  await act(async () => vi.advanceTimersByTime(60_000));
  expect(opened).toHaveLength(1);
});

it("says the Fleet Node is at capacity and starts when a view frees up", async () => {
  const { opened } = renderDialog();
  const message =
    "This container's Fleet Node is showing as many live ASIC views as it can. This view starts when another one closes.";
  await act(async () => opened[0].fail(new ConnectError(message, Code.ResourceExhausted)));
  expect(screen.getByText(message)).toBeInTheDocument();
  expect(screen.queryByText("Live ASIC readings stopped")).toBeNull();
  await act(async () => vi.advanceTimersByTime(BOARD_ASIC_STREAM_RETRY_MS));
  expect(opened).toHaveLength(2);
  await act(async () => opened[1].push(boardAsicFrame()));
  expect(screen.getByRole("status")).toHaveTextContent("Live.");
  expect(screen.queryByText(message)).toBeNull();
});

const slot = create(HashboardSlotSchema, {
  position: 3,
  occupancy: "occupied",
  observedAt: timestampFromMs(start),
  board: { hashboardId: 101n, installationId: 1010n, position: 3 },
});

it("offers live ASIC readings to container viewers only", async () => {
  mocks.permissions = ["site:read", "fleet:read"];
  const { unmount } = render(<HashboardPoolAction deviceIdentifier="north" slot={slot} />);
  expect(screen.queryByRole("button", { name: "Actions for hashboard 3" })).toBeNull();
  unmount();

  mocks.permissions = ["site:read", "fleet:read", "miner:read"];
  render(<HashboardPoolAction deviceIdentifier="north" slot={slot} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for hashboard 3" }));
  await flush();
  expect(screen.getByText("View ASIC readings")).toBeInTheDocument();
  expect(screen.queryByText("Blink LEDs")).toBeNull();
});

it("streams the board captured at the click, not one swapped into the slot later", async () => {
  mocks.permissions = ["site:read", "fleet:read", "miner:read"];
  const calls: { board?: { hashboardId: bigint; installationId: bigint }; signal: AbortSignal }[] = [];
  const stream = vi.spyOn(containersClient, "streamBoardASICReadings").mockImplementation(((
    request: { board?: { hashboardId: bigint; installationId: bigint } },
    options: { signal: AbortSignal },
  ) => {
    calls.push({ board: request.board, signal: options.signal });
    return (async function* () {
      await new Promise<void>((resolve) => options.signal.addEventListener("abort", () => resolve()));
      yield* [];
    })();
  }) as never);

  const view = render(<HashboardPoolAction deviceIdentifier="north" slot={slot} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for hashboard 3" }));
  await flush();
  fireEvent.click(screen.getByText("View ASIC readings"));
  await flush();
  expect(calls).toHaveLength(1);
  expect(calls[0].board).toEqual({ hashboardId: 101n, installationId: 1010n });

  // A refresh puts a replacement board in the slot while the dialog is open.
  const swapped = create(HashboardSlotSchema, {
    position: 3,
    occupancy: "occupied",
    observedAt: timestampFromMs(start),
    board: { hashboardId: 202n, installationId: 2020n, position: 3 },
  });
  view.rerender(<HashboardPoolAction deviceIdentifier="north" slot={swapped} />);
  await flush();
  expect(calls).toHaveLength(1);
  expect(screen.getByText("Board 3 ASIC readings")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  await flush();
  expect(calls[0].signal.aborted).toBe(true);
  stream.mockRestore();
});
