import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { containersClient } from "@/protoFleet/api/clients";
import type { StreamBoardASICReadingsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

export type BoardAsicStreamTarget = { hashboardId: bigint; installationId: bigint };
export type OpenBoardAsicStream = (
  board: BoardAsicStreamTarget,
  signal: AbortSignal,
) => AsyncIterable<StreamBoardASICReadingsResponse>;

const openBoardAsicStream: OpenBoardAsicStream = (board, signal) =>
  containersClient.streamBoardASICReadings({ board }, { signal });

// Reconnect delays after a stream fails. A frame resets the delay.
export const BOARD_ASIC_STREAM_RETRY_MS = 2_000;
export const BOARD_ASIC_STREAM_MAX_RETRY_MS = 30_000;

// Retrying cannot change these outcomes; the board must be reopened from its module.
const FINAL_CODES = new Set([
  Code.FailedPrecondition,
  Code.Unimplemented,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.InvalidArgument,
  Code.NotFound,
]);

const sameSample = (previous: StreamBoardASICReadingsResponse | undefined, next: StreamBoardASICReadingsResponse) =>
  !!previous?.observedAt &&
  !!next.observedAt &&
  previous.observedAt.seconds === next.observedAt.seconds &&
  previous.observedAt.nanos === next.observedAt.nanos;

export type BoardAsicStreamState = {
  frame?: StreamBoardASICReadingsResponse;
  /**
   * Local time the controller's sample last advanced. A controller that keeps
   * resending one sample does not move it, and comparing sample times to each
   * other avoids depending on the controller's clock.
   */
  freshAt?: number;
  error?: string;
  /** No reconnect follows `error`. */
  final: boolean;
};

/**
 * Follows one installed board's live ASIC readings while mounted. The server
 * ends each stream after a few minutes so permissions and the board's
 * placement are re-checked; that clean end reconnects at once. Only that cap
 * ends a stream cleanly: a controller that closes it or goes silent is a
 * failure. Failures keep the last frame and reconnect with capped backoff
 * unless retrying cannot help.
 */
export function useBoardAsicStream(
  board: BoardAsicStreamTarget,
  open: OpenBoardAsicStream = openBoardAsicStream,
): BoardAsicStreamState {
  const [state, setState] = useState<BoardAsicStreamState>({ final: false });
  const openRef = useRef(open);
  useEffect(() => {
    openRef.current = open;
  }, [open]);
  const { hashboardId, installationId } = board;

  useEffect(() => {
    const controller = new AbortController();
    let retryMs = BOARD_ASIC_STREAM_RETRY_MS;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const follow = async () => {
      let frames = 0;
      try {
        for await (const frame of openRef.current({ hashboardId, installationId }, controller.signal)) {
          if (controller.signal.aborted) return;
          frames += 1;
          retryMs = BOARD_ASIC_STREAM_RETRY_MS;
          setState((prev) => ({
            frame,
            freshAt: prev.freshAt !== undefined && sameSample(prev.frame, frame) ? prev.freshAt : Date.now(),
            final: false,
          }));
        }
        if (controller.signal.aborted) return;
        if (frames > 0) {
          void follow();
          return;
        }
        setState((prev) => ({ ...prev, error: "The controller closed the stream before sending readings." }));
      } catch (cause) {
        if (controller.signal.aborted) return;
        const final = FINAL_CODES.has(ConnectError.from(cause).code);
        setState((prev) => ({
          ...prev,
          error: getErrorMessage(cause, "Live ASIC readings are unavailable."),
          final,
        }));
        if (final) return;
      }
      timer = setTimeout(() => void follow(), retryMs);
      retryMs = Math.min(retryMs * 2, BOARD_ASIC_STREAM_MAX_RETRY_MS);
    };
    void follow();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [hashboardId, installationId]);

  return state;
}
