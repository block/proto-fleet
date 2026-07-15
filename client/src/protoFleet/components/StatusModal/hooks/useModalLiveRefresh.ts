import { useCallback, useEffect, useRef, useState } from "react";

/** Cadence of the recurring refresh while the modal is open and foregrounded. */
export const MODAL_REFRESH_INTERVAL_MS = 10_000;

/**
 * After this long without any user interaction inside the document, the poll
 * pauses so an abandoned-but-open modal doesn't hammer the backend forever.
 * Any interaction (or an explicit `resume()`) restarts it with an immediate tick.
 */
export const MODAL_IDLE_CEILING_MS = 10 * 60_000;

/** Interactions that count as "the operator is still watching this modal". */
const INTERACTION_EVENTS = ["mousemove", "keydown", "click", "scroll"] as const;

interface UseModalLiveRefreshOptions {
  /** Poll only while true (typically `isVisible && !!deviceId`). */
  enabled: boolean;
  /**
   * Runs immediately on open, on each tick, and on foreground/idle resume.
   * Receives an `AbortSignal` that fires when this loop is torn down (modal
   * close, `restartKey` change, unmount); an async tick must check
   * `signal.aborted` after awaiting and skip any shared side effects (e.g.
   * merging into a shared store) so a late response can't clobber newer state.
   */
  onTick: (signal: AbortSignal) => void | Promise<void>;
  /**
   * Changing this restarts the loop with an immediate tick — pass the device id
   * so switching the modal's subject fetches fresh data right away.
   */
  restartKey?: string;
  intervalMs?: number;
  idleCeilingMs?: number;
}

interface UseModalLiveRefreshReturn {
  /** True once the idle ceiling has been hit and the poll has stopped. */
  isPaused: boolean;
  /** Manually resume a poll paused by the idle ceiling (immediate tick). */
  resume: () => void;
}

/**
 * Drives a modal's live-refresh loop: one immediate fetch on open, then a
 * recurring tick. Ticks are suspended while the tab is hidden and the whole
 * loop pauses after an idle ceiling. All timers and listeners are torn down
 * when the modal closes (`enabled` goes false) or the component unmounts.
 *
 * The loop is intentionally self-contained so `StatusModal` stays declarative
 * and the lifecycle is testable in isolation.
 */
export const useModalLiveRefresh = ({
  enabled,
  onTick,
  restartKey,
  intervalMs = MODAL_REFRESH_INTERVAL_MS,
  idleCeilingMs = MODAL_IDLE_CEILING_MS,
}: UseModalLiveRefreshOptions): UseModalLiveRefreshReturn => {
  const [isPaused, setIsPaused] = useState(false);

  // Keep the latest tick without restarting the loop when its identity changes.
  const onTickRef = useRef(onTick);
  onTickRef.current = onTick;

  // Filled in by the active effect so the stable `resume` can reach its internals.
  const resumeRef = useRef<() => void>(() => {});
  const resume = useCallback(() => resumeRef.current(), []);

  useEffect(() => {
    if (!enabled) {
      // Reset when the modal closes so a fresh open never starts paused.
      // eslint-disable-next-line react-hooks/set-state-in-effect -- syncing UI state to the (torn-down) external timer loop
      setIsPaused(false);
      return;
    }

    let interval: ReturnType<typeof setInterval> | null = null;
    let lastInteraction = Date.now();
    let paused = false;
    let inFlight = false;
    // Aborted on cleanup so a tick still awaiting when this loop tears down can
    // be ignored by onTick instead of merging stale data after the next loop's
    // fresh fetch. The in-flight guard alone is local to this run and can't stop
    // an already-started async tick from resuming.
    const abortController = new AbortController();

    const runTick = () => {
      // Skip work the operator can't see; the visibility handler catches them up.
      if (document.visibilityState !== "visible") return;
      // Serialize ticks: if a refresh is slower than the cadence, don't start a
      // second one. Overlapping ticks would let a slow older response merge
      // after a newer one and regress the modal/list back to stale status.
      if (inFlight) return;
      const result = onTickRef.current(abortController.signal);
      // Only an async tick can still be running when the next interval fires; a
      // synchronous tick has already completed, so nothing to guard.
      if (result && typeof (result as Promise<void>).then === "function") {
        inFlight = true;
        (result as Promise<void>).finally(() => {
          inFlight = false;
        });
      }
    };

    const stopInterval = () => {
      if (interval !== null) {
        clearInterval(interval);
        interval = null;
      }
    };

    const startInterval = () => {
      stopInterval();
      interval = setInterval(() => {
        // Don't let hidden-tab time advance the idle ceiling. If the loop is
        // enabled while the tab is already hidden, no visibilitychange fires to
        // stop the interval, so guard here too — otherwise it could pause mid
        // background and then refuse the catch-up tick on return.
        if (document.visibilityState !== "visible") return;
        if (Date.now() - lastInteraction >= idleCeilingMs) {
          paused = true;
          stopInterval();
          setIsPaused(true);
          return;
        }
        runTick();
      }, intervalMs);
    };

    const start = () => {
      paused = false;
      setIsPaused(false);
      lastInteraction = Date.now();
      runTick();
      startInterval();
    };

    const handleVisibility = () => {
      if (document.visibilityState !== "visible") {
        stopInterval();
        return;
      }
      // Back in the foreground — reset the idle baseline so time spent hidden
      // doesn't count against the ceiling, then catch up and resume the cadence.
      if (!paused) {
        lastInteraction = Date.now();
        runTick();
        startInterval();
      }
    };

    const handleInteraction = () => {
      lastInteraction = Date.now();
      if (paused) start();
    };

    resumeRef.current = () => {
      if (paused) start();
    };

    start();

    document.addEventListener("visibilitychange", handleVisibility);
    // Capture phase so element-level "scroll" (which does not bubble) still
    // counts as interaction when the operator scrolls inside the modal.
    INTERACTION_EVENTS.forEach((event) =>
      document.addEventListener(event, handleInteraction, { capture: true, passive: true }),
    );

    return () => {
      abortController.abort();
      stopInterval();
      document.removeEventListener("visibilitychange", handleVisibility);
      INTERACTION_EVENTS.forEach((event) => document.removeEventListener(event, handleInteraction, { capture: true }));
      resumeRef.current = () => {};
    };
  }, [enabled, restartKey, intervalMs, idleCeilingMs]);

  return { isPaused, resume };
};

export default useModalLiveRefresh;
