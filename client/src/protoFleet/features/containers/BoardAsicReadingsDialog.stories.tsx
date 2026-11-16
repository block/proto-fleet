import { Code, ConnectError } from "@connectrpc/connect";
import type { Meta, StoryObj } from "@storybook/react";
import BoardAsicReadingsDialog from "./BoardAsicReadingsDialog";
import { boardAsicFrame } from "./boardAsicStreamFixtures";
import type { OpenBoardAsicStream } from "./useBoardAsicStream";

const wait = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => {
      clearTimeout(timer);
      resolve();
    });
  });

// A warm board with a hot corner and one quarantined ASIC, refreshed each second.
const temperature = (tick: number) => (index: number) => {
  const row = Math.floor(index / 26);
  const column = index % 26;
  const hot = Math.max(0, 6 - Math.hypot(row - 9, column - 20)) * 5;
  return 54 + ((index * 7 + tick) % 7) + row * 0.5 + hot;
};

const live: OpenBoardAsicStream = async function* (_board, signal) {
  for (let tick = 0; !signal.aborted; tick += 1) {
    yield boardAsicFrame({ observedAt: Date.now(), temperature: temperature(tick), quarantined: [255] });
    await wait(1_000, signal);
  }
};

// The controller keeps sending frames, but its sample time stopped advancing.
const stalled: OpenBoardAsicStream = async function* (_board, signal) {
  const observedAt = Date.now();
  while (!signal.aborted) {
    yield boardAsicFrame({ observedAt, temperature: temperature(0), quarantined: [255] });
    await wait(1_000, signal);
  }
};

// The stream is open but the first frame has not arrived.
const connecting: OpenBoardAsicStream = async function* (_board, signal) {
  await new Promise<void>((resolve) => signal.addEventListener("abort", () => resolve()));
  yield* [];
};

const interrupted: OpenBoardAsicStream = async function* () {
  yield boardAsicFrame({ observedAt: Date.now() - 20_000, temperature: temperature(0), quarantined: [255] });
  throw new ConnectError("the controller stopped sending ASIC readings: board offline", Code.Unavailable);
};

const fleetNode: OpenBoardAsicStream = async function* () {
  yield* [];
  throw new ConnectError(
    "Live ASIC readings are not yet available for containers reached through a Fleet Node.",
    Code.FailedPrecondition,
  );
};

const meta = {
  title: "Proto Fleet/Containers/Board ASIC readings",
  component: BoardAsicReadingsDialog,
  args: {
    board: { hashboardId: 101n, installationId: 1010n },
    position: 3,
    onClose: () => undefined,
    openStream: live,
  },
  argTypes: { board: { control: false }, openStream: { control: false } },
} satisfies Meta<typeof BoardAsicReadingsDialog>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Connecting: Story = { args: { openStream: connecting } };
export const Live: Story = {};
export const SampleStalled: Story = { args: { openStream: stalled } };
export const Reconnecting: Story = { args: { openStream: interrupted } };
export const ThroughFleetNode: Story = { args: { openStream: fleetNode } };
