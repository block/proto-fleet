import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { OpenBoardAsicStream } from "./useBoardAsicStream";
import {
  type StreamBoardASICReadingsResponse,
  StreamBoardASICReadingsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";

/** A CT1-like board frame: `stacks` stacks of `perStack` ASICs reported as one long row each. */
export const boardAsicFrame = ({
  observedAt = Date.UTC(2026, 10, 14, 8, 0),
  stacks = 6,
  perStack = 52,
  temperature = (index: number) => 58 + ((index * 7) % 29),
  quarantined = [] as number[],
} = {}): StreamBoardASICReadingsResponse =>
  create(StreamBoardASICReadingsResponseSchema, {
    hashboardId: 101n,
    observedAt: timestampFromMs(observedAt),
    asicsPerStack: perStack,
    asics: Array.from({ length: stacks * perStack }, (_, index) => ({
      index,
      temperatureC: temperature(index),
      hashrateGhs: 450,
      voltageV: 0.32,
      frequencyMhz: 500,
      passRatePercent: 99.5,
      thermalQuarantined: quarantined.includes(index),
      position: {
        stackId: Math.floor(index / perStack),
        stackIndex: index % perStack,
        x: index % perStack,
        y: Math.floor(index / perStack),
      },
    })),
  });

type Channel = {
  push: (frame: StreamBoardASICReadingsResponse) => void;
  end: () => void;
  fail: (error: unknown) => void;
  signal: AbortSignal;
};

/** A stream the test drives; each open records its own channel. */
export const controllableBoardAsicStream = () => {
  const opened: Channel[] = [];
  const open: OpenBoardAsicStream = (_board, signal) => {
    const queue: Array<IteratorResult<StreamBoardASICReadingsResponse> | { error: unknown }> = [];
    let wake: (() => void) | undefined;
    const add = (item: (typeof queue)[number]) => {
      queue.push(item);
      wake?.();
    };
    signal.addEventListener("abort", () => add({ error: new DOMException("aborted", "AbortError") }));
    opened.push({
      push: (frame) => add({ value: frame, done: false }),
      end: () => add({ value: undefined, done: true }),
      fail: (error) => add({ error }),
      signal,
    });
    return {
      [Symbol.asyncIterator]: () => ({
        next: async () => {
          while (queue.length === 0) await new Promise<void>((resolve) => (wake = resolve));
          const item = queue.shift()!;
          if ("error" in item) throw item.error;
          return item;
        },
      }),
    };
  };
  return { open, opened };
};
