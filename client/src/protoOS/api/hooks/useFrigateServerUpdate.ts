import { useEffect, useRef, useState } from "react";
import {
  type FrigatePackage,
  type FrigateRollout,
  type FrigateServerUpdate,
  type FrigateUpdateAction,
  FrigateUpdateError,
  type FrigateUpdatePlan,
  type FrigateUpdateRecord,
  FrigateUpdateRefusedError,
  type FrigateUpdateStatus,
  newFrigateJobId,
  releaseOf,
} from "../adapters/frigateServerUpdate";
import { describeFrigateUpdateProblem, type FrigateUpdateMessage } from "../adapters/frigateUpdateMessages";
import {
  isFrigateUpdateKey,
  readFrigateUpdate,
  readFrigateUpdates,
  removeFrigateUpdate,
  saveFrigateUpdate,
} from "./frigateUpdateStorage";
import type { ReleaseFiles, ReleasePackageFileKey } from "@/shared/features/frigateUpdate/releaseFiles";
import { usePoll } from "@/shared/hooks/usePoll";

const READ_TIMEOUT_MS = 15_000;
// A package can be hundreds of megabytes over a slow site link, copied once to each controller.
const STAGE_TIMEOUT_MS = 30 * 60_000;
const START_TIMEOUT_MS = 60_000;
// Past the timeout plus this, a Start that was sent can no longer be in flight on the controller.
const START_MARGIN_MS = 15_000;
const POLL_MS = 3_000;

const NOT_STARTED: FrigateUpdateMessage = {
  title: "The controllers have no record of this update starting. Start it again, or discard it.",
};
const START_UNCONFIRMED = "The controller didn't confirm the start. Checking whether the update started…";
const DISCARDED_ELSEWHERE = "This update was discarded in another tab, so it was not started.";
const ALREADY_TRACKED = "Another software update is already tracked in this browser. Check its status first.";
const OTHER_UPDATE_ACTIVE =
  "Another software update is running on the controllers. Discard this copy once that update has finished.";

export const frigateUpdateMessage = (error: unknown): FrigateUpdateMessage =>
  error instanceof FrigateUpdateError
    ? describeFrigateUpdateProblem(error.code, error.message)
    : { title: error instanceof Error && error.message ? error.message : "The controller didn't answer." };

export type FrigateUpdateActivity =
  "checking" | "reading" | "staging" | "starting" | "retrying" | "cancelling" | "discarding";

/** A rollout worth following: still changing, or failed with Frigate possibly stopped until it is retried. */
const recoverable = (rollout?: FrigateRollout) => !!rollout && (!rollout.ended || rollout.retryable);

/** A record whose update was started, or may have been: it is followed by its own job id. */
const wasStarted = (record?: FrigateUpdateRecord) => record?.step === "starting" || record?.step === "started";

const read = (signal: AbortSignal) => ({ signal: AbortSignal.any([signal, AbortSignal.timeout(READ_TIMEOUT_MS)]) });

export function useFrigateServerUpdate(update: FrigateServerUpdate, deviceId: string) {
  // One update is shown at a time. Another saved record is adopted once this one is released.
  const [record, setRecord] = useState(() => readFrigateUpdate(deviceId));
  // The release files chosen so far, by kind. `pkg` is set once all three are chosen and agree with each other.
  const [files, setFiles] = useState<ReleaseFiles<ReleasePackageFileKey>>({});
  const [pkg, setPkg] = useState<FrigatePackage>();
  const [plan, setPlan] = useState<FrigateUpdatePlan>();
  const [status, setStatus] = useState<FrigateUpdateStatus>();
  // A rollout this browser didn't start, found running on the controllers. It is followed but never saved.
  const [observedJobId, setObservedJobId] = useState<string>();
  const [lastAnswer, setLastAnswer] = useState<Date>();
  const [unanswered, setUnanswered] = useState<string>();
  const [alerts, setAlerts] = useState<FrigateUpdateMessage[]>([]);
  const [activity, setActivity] = useState<FrigateUpdateActivity>();
  const [stagePercent, setStagePercent] = useState(0);
  // The job whose Discard failed, so the dialog can offer to stop tracking a copy that can't be removed.
  const [undiscarded, setUndiscarded] = useState<string>();
  const request = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  // The latest record, for operations that update it more than once.
  const current = useRef(record);
  current.current = record;
  // A retry or cancel key for a rollout without a record, kept like a record's `pendingAction`.
  const observedAction = useRef<({ jobId: string } & FrigateUpdateAction) | undefined>(undefined);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);

  // A copy that hasn't started doesn't hide a rollout found on the controllers: that one is followed, and the copy
  // is shown again once it is released.
  const following = record && wasStarted(record) ? record.jobId : observedJobId;
  const rollout = following && status?.rollout?.jobId === following ? status.rollout : undefined;
  const busy = !!activity;
  // A retryable failure can still change: a retry whose answer was lost, or one sent from another client.
  const settled = !!rollout?.ended && !rollout.retryable;

  /** Rewrites this job's record unless another tab already released it. */
  const retrack = (next: FrigateUpdateRecord) => {
    const saved = readFrigateUpdates(deviceId);
    const own = saved.some((candidate) => candidate.jobId === next.jobId);
    if (own) saveFrigateUpdate(deviceId, next);
    current.current = own ? next : saved[0];
    if (mounted.current) setRecord(current.current);
  };

  /** Re-reads the saved record, which another tab may have advanced or removed since this one last looked. */
  const sync = () => {
    const saved = readFrigateUpdates(deviceId);
    const next = saved.find((candidate) => candidate.jobId === current.current?.jobId) ?? saved[0];
    current.current = next;
    if (mounted.current) setRecord(next);
    return next;
  };
  const syncRef = useRef(sync);
  syncRef.current = sync;
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (isFrigateUpdateKey(deviceId, event.key)) syncRef.current();
    };
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, [deviceId]);

  const pendingAction = (jobId: string) =>
    current.current?.jobId === jobId
      ? current.current.pendingAction
      : observedAction.current?.jobId === jobId
        ? observedAction.current
        : undefined;
  const setPendingAction = (jobId: string, action?: FrigateUpdateAction) => {
    const tracked = current.current;
    if (tracked?.jobId === jobId) retrack({ ...tracked, pendingAction: action });
    else observedAction.current = action && { jobId, ...action };
  };
  /**
   * Drops a retry or cancel key once the rollout shows it was used, so the next press is a new request. The action no
   * longer being offered shows that. So does a rollout that changed since the key was sent: a retry that was accepted
   * and failed again between two status answers looks retryable throughout, and replaying its key would do nothing.
   */
  const settleAction = (seen: FrigateRollout) => {
    const pending = pendingAction(seen.jobId);
    if (!pending) return;
    const offered = pending.kind === "retry" ? seen.retryable : seen.cancellable;
    const changed = pending.since !== undefined && seen.updatedAt !== undefined && seen.updatedAt !== pending.since;
    if (!offered || changed) setPendingAction(seen.jobId, undefined);
  };

  /** Settles a Start whose outcome isn't known yet from a status answer for its job. */
  const settleStart = (starting: FrigateUpdateRecord, result: FrigateUpdateStatus) => {
    if (result.rollout) retrack({ ...starting, step: "started", notStarted: undefined, startSettlesAt: undefined });
    // Frigate saves the rollout only after its checks, so "no rollout" counts once Start can't be in flight any more.
    else if (Date.now() >= (starting.startSettlesAt ?? 0))
      retrack({
        ...starting,
        step: "staged",
        notStarted: starting.notStarted ?? NOT_STARTED,
        startSettlesAt: undefined,
      });
    else return;
    // The start is no longer unconfirmed, so its notice goes.
    if (mounted.current) setAlerts((shown) => shown.filter((alert) => alert.title !== START_UNCONFIRMED));
  };

  // An update the operator stopped tracking isn't picked up again behind their back. Opening the dialog still shows it.
  const released = useRef<string | undefined>(undefined);

  /**
   * Follows what a status answer with no job id found. This browser's own unstarted copy turns out to have started
   * (in another tab, or by a Start whose answer was lost). Any other rollout worth following is followed.
   */
  const adoptFound = (found: FrigateUpdateStatus) => {
    const tracked = current.current;
    if (!found.rollout || wasStarted(tracked)) return false;
    if (tracked && found.rollout.jobId === tracked.jobId) {
      retrack({ ...tracked, step: "started", notStarted: undefined, startSettlesAt: undefined });
    } else {
      if (!recoverable(found.rollout) || found.rollout.jobId === released.current) return false;
      setObservedJobId(found.rollout.jobId);
    }
    setStatus(found);
    setLastAnswer(new Date());
    return true;
  };

  // With no started update tracked here, find a rollout started elsewhere so this page follows it instead of offering
  // another. A copy that hasn't started doesn't stop the search: Frigate may be stopped by someone else's update.
  // A retryable failure is adopted too: Frigate may be left stopped, and only Retry or Cancel recovers it. A lookup
  // that gets no answer (Frigate may be restarting) is asked again until one arrives. A refusal is an answer: a
  // controller that doesn't serve updates has no rollout to follow.
  const [lookedUp, setLookedUp] = useState(false);
  usePoll({
    params: update,
    enabled: !wasStarted(record) && !observedJobId && !lookedUp,
    poll: true,
    pollIntervalMs: POLL_MS,
    fetchData: async (signal) => {
      const result = await update
        .status(undefined, read(signal))
        .catch((failure) =>
          failure instanceof FrigateUpdateRefusedError ? { servedByMaintenance: false } : undefined,
        );
      if (signal.aborted || !mounted.current || !result) return;
      setLookedUp(true);
      adoptFound(result);
    },
  });

  usePoll({
    params: following,
    enabled: !!following && !busy && !settled,
    poll: true,
    pollIntervalMs: POLL_MS,
    fetchData: async (signal) => {
      if (!following) return;
      try {
        const result = await update.status(following, read(signal));
        if (signal.aborted || !mounted.current) return;
        setUnanswered(undefined);
        setLastAnswer(new Date());
        const tracked = current.current;
        if (tracked?.jobId === following && tracked.step === "starting") settleStart(tracked, result);
        if (result.rollout) settleAction(result.rollout);
        setStatus(result);
      } catch (failure) {
        if (!signal.aborted) setUnanswered(frigateUpdateMessage(failure).title);
      }
    },
  });

  const running = useRef<FrigateUpdateActivity | undefined>(undefined);
  const run = async (kind: FrigateUpdateActivity, operation: (signal: AbortSignal) => Promise<void>) => {
    // A read only informs the dialog, so a newer request replaces it. Anything that writes runs to its answer.
    if (request.current) {
      if (running.current !== "checking" && running.current !== "reading") return;
      request.current.abort();
    }
    const controller = new AbortController();
    request.current = controller;
    running.current = kind;
    setActivity(kind);
    setAlerts([]);
    try {
      await operation(controller.signal);
    } catch (failure) {
      if (mounted.current && !controller.signal.aborted) setAlerts([frigateUpdateMessage(failure)]);
    } finally {
      if (request.current === controller) {
        request.current = null;
        running.current = undefined;
        if (mounted.current) setActivity(undefined);
      }
    }
  };

  const adoptActive = (checked: FrigateUpdatePlan) => {
    const tracked = current.current;
    if (checked.activeJobId && !wasStarted(tracked) && checked.activeJobId !== tracked?.jobId)
      setObservedJobId(checked.activeJobId);
  };

  /** Whether this job's record is still saved. Another tab removes it when the operator discards the update there. */
  const stillSaved = (jobId: string) => readFrigateUpdates(deviceId).some((saved) => saved.jobId === jobId);

  /**
   * Looks for a rollout to follow, then reads both controllers' support, with the chosen package if there is one.
   * The rollout is asked for first and separately, so a failed support read can't hide it: while Frigate is stopped
   * only its maintenance service answers, and the simulator's serves status but not support.
   */
  const check = () =>
    run("checking", async (signal) => {
      if (!current.current) {
        const found = await update.status(undefined, read(signal)).catch(() => undefined);
        signal.throwIfAborted();
        if (found && adoptFound(found)) return;
      }
      const checked = await update.plan(pkg, read(signal));
      signal.throwIfAborted();
      setPlan(checked);
      adoptActive(checked);
    });

  /**
   * Takes the chosen release files. A complete release is read and checked against the controllers. An incomplete
   * one has no package, so a package read earlier is dropped and the controllers are checked without it.
   */
  const choose = (next: ReleaseFiles<ReleasePackageFileKey>) => {
    setFiles(next);
    const { deb, statement, signature } = next;
    if (deb && statement && signature)
      return run("reading", async (signal) => {
        setPkg(undefined);
        const selected = await update.readPackage([deb, statement, signature], { signal });
        const checked = await update.plan(selected, read(signal));
        signal.throwIfAborted();
        setPkg(selected);
        setPlan(checked);
        adoptActive(checked);
      });
    if (!pkg && running.current !== "reading") {
      // No package was read or is being read, so only a message about the earlier files is out of date.
      setAlerts([]);
      return Promise.resolve();
    }
    return run("checking", async (signal) => {
      setPkg(undefined);
      const checked = await update.plan(undefined, read(signal));
      signal.throwIfAborted();
      setPlan(checked);
      adoptActive(checked);
    });
  };

  /** Shows why a file selection can't be used. The chosen files stay as they were. */
  const reject = (message: string) => setAlerts([{ title: message }]);

  const startTracked = async (staged: FrigateUpdateRecord, signal: AbortSignal) => {
    // Saved before Start, so a lost answer is settled by status and Start is never sent blindly again.
    const starting: FrigateUpdateRecord = {
      ...staged,
      step: "starting",
      notStarted: undefined,
      startSettlesAt: Date.now() + START_TIMEOUT_MS + START_MARGIN_MS,
    };
    retrack(starting);
    // Discarded in another tab since it was copied: nothing here tracks it any more, so it must not start.
    if (current.current?.jobId !== starting.jobId) throw new Error(DISCARDED_ELSEWHERE);
    let failure: unknown;
    try {
      const started = await update.start(starting, {
        signal: AbortSignal.any([signal, AbortSignal.timeout(START_TIMEOUT_MS)]),
      });
      retrack({ ...starting, step: "started", startSettlesAt: undefined });
      setStatus({ rollout: started, servedByMaintenance: false });
      setLastAnswer(new Date());
      return;
    } catch (error) {
      failure = error;
    }
    if (signal.aborted) throw failure;
    // Only status can say that no rollout exists for this job. A refusal changed nothing, but an idempotency conflict,
    // for one, is returned when a rollout is already there. After any other failure the write may have landed, so the
    // record stays "starting" until Start can't be in flight any more. The reason is kept for when it settles.
    const refused = failure instanceof FrigateUpdateRefusedError;
    retrack({
      ...starting,
      notStarted: failure instanceof FrigateUpdateError ? frigateUpdateMessage(failure) : undefined,
      startSettlesAt: refused ? undefined : starting.startSettlesAt,
    });
    const unsettled = () => {
      const latest = current.current;
      return latest?.jobId === starting.jobId && latest.step === "starting" ? latest : undefined;
    };
    if (!unsettled()) return;
    const result = await update.status(starting.jobId, read(signal)).catch(() => undefined);
    const latest = unsettled();
    if (result && latest && mounted.current) {
      settleStart(latest, result);
      setStatus(result);
      setLastAnswer(new Date());
    }
    if (!refused && unsettled()) throw Object.assign(new Error(START_UNCONFIRMED), { cause: failure });
  };

  /** Copies the chosen package to both controllers, then starts the update. Resumes an interrupted copy. */
  const install = () =>
    run("staging", async (signal) => {
      if (!pkg) return;
      let job = sync();
      if (job && !(job.step === "staging" && job.targetVersion === pkg.version)) return;
      if (!job) {
        const other = readFrigateUpdate(deviceId);
        if (other) {
          setRecord(other);
          throw new Error(ALREADY_TRACKED);
        }
        // Support can change while the dialog is open, so read it again just before copying.
        const checked = await update.plan(pkg, read(signal));
        setPlan(checked);
        adoptActive(checked);
        if (checked.blockers.length) return;
        job = {
          jobId: newFrigateJobId(),
          idempotencyKey: newFrigateJobId(),
          targetVersion: pkg.version,
          packageName: pkg.deb.name,
          instanceIds: checked.controllers.map((controller) => controller.instanceId).sort(),
          fingerprints: {},
          release: releaseOf(pkg),
          step: "staging",
        };
        // A storage failure refuses the update rather than losing recovery information.
        saveFrigateUpdate(deviceId, job);
        current.current = job;
        setRecord(job);
      }
      setStagePercent(0);
      const { jobId } = job;
      let fingerprints = job.fingerprints;
      // Stops the copy as soon as the record is gone, so a resume can't recreate a stage the operator discarded.
      const copying = new AbortController();
      const copied = update
        .stage(
          pkg,
          job,
          (percent, learned) => {
            if (mounted.current) setStagePercent(percent);
            const saved = current.current;
            if (saved?.jobId !== jobId || !stillSaved(jobId)) return copying.abort();
            if (Object.keys(learned).length !== Object.keys(fingerprints).length) {
              fingerprints = learned;
              retrack({ ...saved, fingerprints });
            }
          },
          { signal: AbortSignal.any([signal, copying.signal, AbortSignal.timeout(STAGE_TIMEOUT_MS)]) },
        )
        .then((result) => (fingerprints = result));
      try {
        await copied;
      } catch (failure) {
        if (stillSaved(jobId)) throw failure;
      }
      if (!stillSaved(jobId)) {
        // Whatever reached the controllers after the discard is removed again.
        await update.discard({ ...job, fingerprints }, read(signal)).catch(() => undefined);
        current.current = readFrigateUpdate(deviceId);
        if (mounted.current) setRecord(current.current);
        throw new Error(DISCARDED_ELSEWHERE);
      }
      const staged: FrigateUpdateRecord = { ...(current.current ?? job), fingerprints, step: "staged" };
      retrack(staged);
      running.current = "starting";
      setActivity("starting");
      await startTracked(staged, signal);
    });

  /** Starts a staged update again with the same idempotency key, so a rollout that did start is returned, not doubled. */
  const start = () =>
    run("starting", async (signal) => {
      // Another tab may have started or discarded it since this one last looked.
      const shown = current.current;
      const tracked = sync();
      if (shown && tracked?.jobId !== shown.jobId) throw new Error(DISCARDED_ELSEWHERE);
      if (tracked?.step === "staged") await startTracked(tracked, signal);
    });

  /** Clears what is shown for a job whose record is gone, and shows the next saved update if there is one. */
  const forget = (jobId: string | undefined) => {
    released.current = jobId;
    setLookedUp(false);
    current.current = readFrigateUpdate(deviceId);
    setRecord(current.current);
    setStatus(undefined);
    setObservedJobId(undefined);
    setUnanswered(undefined);
    setLastAnswer(undefined);
    setAlerts([]);
    setUndiscarded(undefined);
    observedAction.current = undefined;
  };

  /** Stops showing what is shown now. A rollout found on the controllers goes first, and a saved copy stays. */
  const release = () => {
    const tracked = current.current;
    if (observedJobId && !wasStarted(tracked)) return forget(observedJobId);
    if (tracked) removeFrigateUpdate(deviceId, tracked.jobId);
    forget(tracked?.jobId);
  };

  /** Removes a copied package that never started. The record is kept unless every controller confirms. */
  const discard = () =>
    run("discarding", async (signal) => {
      const tracked = sync();
      if (!tracked || (tracked.step !== "staging" && tracked.step !== "staged")) return;
      try {
        await update.discard(tracked, read(signal));
      } catch (failure) {
        if (signal.aborted) throw failure;
        if (!(failure instanceof FrigateUpdateError && failure.code === "UPDATE_ACTIVE_JOB")) {
          if (mounted.current) setUndiscarded(tracked.jobId);
          throw failure;
        }
        // An update is running. Status says whether it is this job (started in another tab, or by a Start whose
        // answer was lost), which is then followed. If it is someone else's, this copy was never started.
        const own = await update.status(tracked.jobId, read(signal));
        if (own.rollout) {
          retrack({ ...tracked, step: "started", notStarted: undefined });
          setStatus(own);
          setLastAnswer(new Date());
          return;
        }
        const found = await update.status(undefined, read(signal)).catch(() => undefined);
        if (found && mounted.current) adoptFound(found);
        throw Object.assign(new Error(OTHER_UPDATE_ACTIVE), { cause: failure });
      }
      // Only this job's record goes: another tab may have discarded it and saved a new update while this waited.
      removeFrigateUpdate(deviceId, tracked.jobId);
      if (current.current?.jobId === tracked.jobId) forget(tracked.jobId);
      else sync();
    });

  const act = (kind: "retry" | "cancel") =>
    run(kind === "retry" ? "retrying" : "cancelling", async (signal) => {
      const jobId = following;
      if (!jobId) return;
      // The key is kept until status shows the action took effect, so a lost answer, or a second press after an
      // accepted one, replays the same request instead of sending another.
      const pending = pendingAction(jobId);
      const action: FrigateUpdateAction =
        pending?.kind === kind ? pending : { kind, key: newFrigateJobId(), since: rollout?.updatedAt };
      setPendingAction(jobId, action);
      try {
        const answered = await (kind === "retry" ? update.retry : update.cancel)(jobId, action.key, read(signal));
        settleAction(answered);
        setStatus({ rollout: answered, servedByMaintenance: false });
        setLastAnswer(new Date());
      } catch (failure) {
        // A refusal changed nothing, so the next attempt is a new request.
        if (failure instanceof FrigateUpdateRefusedError) setPendingAction(jobId, undefined);
        throw failure;
      }
    });

  /** Stops an in-flight read or copy. Start, retry and cancel can't be stopped once sent. */
  const abort = () => {
    if (activity === "checking" || activity === "reading" || activity === "staging") request.current?.abort();
  };

  return {
    record,
    files,
    pkg,
    plan,
    rollout,
    following,
    /** The controllers answered for the tracked job and had no rollout for it. */
    lost: record?.step === "started" && following === record.jobId && !!status && !status.rollout,
    /** Discard failed for the copy shown, so it may be impossible to remove from here. */
    discardFailed: !!record && undiscarded === record.jobId,
    servedByMaintenance: !!rollout && !!status?.servedByMaintenance,
    lastAnswer,
    unanswered,
    alerts,
    activity,
    busy,
    stagePercent,
    check,
    choose,
    reject,
    install,
    start,
    discard,
    retry: () => act("retry"),
    cancel: () => act("cancel"),
    /** Forgets the tracked or followed update in this browser. The controllers are not asked to change anything. */
    release,
    abort,
  };
}
