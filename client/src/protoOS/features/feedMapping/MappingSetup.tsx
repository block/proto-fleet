import { useState } from "react";
import { type FeedMappingState, useFeedMappingState } from "./feedMappingContext";
import { type MappingBoard, type MappingRun, mappingRunActive } from "@/protoOS/api/adapters/feedMapping";
import { Alert } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";

const phases: Record<string, string> = {
  STARTING: "Starting mapping…",
  ISOLATING: "Turning feeds off before mapping…",
  WAITING_ONLINE: "Waiting for hashboards to come online…",
  WAITING_OFFLINE: "Confirming hashboards are offline…",
  PERSISTING: "Saving hashboard locations…",
  RESTORING: "Restoring power to all feeds…",
  CANCELING: "Canceling mapping and restoring power…",
};
const progressText = (run: MappingRun) => {
  if (run.status === "completed") return "Mapping complete";
  if (run.status === "canceled") return "Mapping canceled";
  if (run.status === "failed") return "Mapping failed";
  return phases[run.phase] ?? "Mapping hashboards…";
};
const activityText = (activity: string) =>
  activity === "Planning" ? "Checking mapping availability…" : `${activity} mapping…`;

const words = (value: string) => value.replaceAll("_", " ").toLowerCase();
const location = (board: MappingBoard) =>
  board.unlocated
    ? "Unassigned"
    : `Tank ${board.tank}, module ${board.module}${board.position ? `, HB${board.position}` : ""}`;
const needsReview = (board: MappingBoard) =>
  board.unlocated ||
  board.conflict ||
  board.reasons.includes("no_feed_assigned") ||
  ["stale", "ambiguous", "deferred"].includes(board.status);

function BoardEvidence({ board }: { board: MappingBoard }) {
  return (
    <details className="border-b border-border-10 py-3">
      <summary className="cursor-pointer text-emphasis-200">
        {board.mac} — {location(board)}
        <span className="ml-2 text-200 text-text-primary-70">
          {board.conflict ? "Mapping conflict" : words(board.status)}
        </span>
      </summary>
      <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-200 break-words">
        <dt>CU1 identity</dt>
        <dd>{board.moduleId || "Unavailable"}</dd>
        {board.expectedLocation ? (
          <>
            <dt>Expected location</dt>
            <dd>{board.expectedLocation}</dd>
          </>
        ) : null}
        <dt>Expected feed</dt>
        <dd>{board.expectedFeed || "Unavailable"}</dd>
        {board.assignedFeed ? (
          <>
            <dt>Assigned feed</dt>
            <dd>{board.assignedFeed}</dd>
          </>
        ) : null}
        <dt>Observed feed</dt>
        <dd>{board.observedFeed || "Unavailable"}</dd>
        {board.switchPort ? (
          <>
            <dt>Switch / port</dt>
            <dd>{board.switchPort}</dd>
          </>
        ) : null}
        {board.verifiedAt ? (
          <>
            <dt>Last verification</dt>
            <dd>{new Date(board.verifiedAt).toLocaleString()}</dd>
          </>
        ) : null}
      </dl>
      {board.reasons.length ? (
        <ul className="mt-3 list-inside list-disc text-200">
          {board.reasons.map((reason) => (
            <li key={reason}>{words(reason)}</li>
          ))}
        </ul>
      ) : null}
      {board.coverage.length ? (
        <p className="mt-3 text-200 text-text-primary-70">
          Incomplete evidence: {board.coverage.map(words).join(", ")}.
        </p>
      ) : null}
    </details>
  );
}

/** Shared primitives match ProtoOS's existing pool and wake callouts. */
export function MappingSetup({ state }: { state: FeedMappingState }) {
  const [open, setOpen] = useState(false);
  const [verify, setVerify] = useState(false);
  const [clearUnknown, setClearUnknown] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const boards = state.assessment?.boards ?? [];
  const unlocated = boards.filter((board) => board.unlocated).length;
  const conflicts = boards.filter((board) => board.conflict).length;
  const affected = boards.filter(needsReview);
  const active = mappingRunActive(state.run);
  const unknown = state.unresolved && !active;
  const unavailable = state.error || state.assessment?.unavailable;
  const visible = !!(affected.length || active || unknown || unavailable || state.run || state.notice);
  const title = active
    ? "Hashboard mapping is running"
    : unknown
      ? "Mapping outcome is unknown"
      : affected.length
        ? "Hashboard mapping needs attention"
        : unavailable
          ? "Hashboard mapping is unavailable"
          : "Hashboard mapping finished";
  const summary = [
    unlocated ? `${unlocated} ${unlocated === 1 ? "hashboard has" : "hashboards have"} no assigned location.` : "",
    conflicts
      ? `${conflicts} ${conflicts === 1 ? "hashboard has" : "hashboards have"} conflicting mapping evidence.`
      : "",
  ]
    .filter(Boolean)
    .join(" ");
  const mode = state.assessment?.mode;
  const verifyBoards = affected.length ? affected : boards;
  const close = () => {
    setOpen(false);
    setVerify(false);
    setClearUnknown(false);
  };

  if (!visible && !open) return null;
  return (
    <>
      {visible ? (
        <div role="status">
          <Callout
            intent={active ? "information" : affected.length || unknown || unavailable ? "danger" : "success"}
            prefixIcon={active ? <ProgressCircular indeterminate /> : <Alert />}
            title={title}
            subtitle={summary || (unavailable ? "The last mapping result may be out of date." : undefined)}
            buttonText={active ? "View progress" : "Refresh mapping"}
            buttonOnClick={() => setOpen(true)}
          />
        </div>
      ) : null}
      {open && !verify && !clearUnknown ? (
        <Dialog
          open
          manageFocus
          title="Hashboard mapping"
          className="tablet:w-180"
          onDismiss={close}
          buttons={[
            { text: "Close", variant: variants.secondary, onClick: close },
            ...(mode === "manual" && !state.unresolved && !active
              ? [
                  {
                    text: "Refresh mapping",
                    variant: variants.primary,
                    disabled: !state.canRemap,
                    loading: !!state.activity,
                    onClick: async () => {
                      const plan = await state.plan();
                      if (plan) await state.start(plan);
                    },
                  },
                ]
              : []),
          ]}
        >
          <div className="flex flex-col gap-4">
            {mode === "manual" && !active && !unknown ? (
              <div className="flex flex-col gap-3 text-300">
                <p>
                  Refreshing starts a new hashboard mapping run. It turns all controllable feeds off, then maps them one
                  at a time.
                </p>
                <p>
                  This changes power usage and reduces hashrate while mapping runs. Some actions will not be available
                  until mapping is complete.
                </p>
                <p>
                  After completion or cancellation, all feeds are set to turn on, including feeds that were off before
                  the run. A failure can leave feeds off.
                </p>
              </div>
            ) : null}
            {active || state.activity ? (
              <div role="status" className="flex items-center gap-3">
                <ProgressCircular indeterminate dataTestId="mapping-progress-spinner" />
                <p className="text-emphasis-200">
                  {state.activity ? activityText(state.activity) : progressText(state.run!)}
                </p>
              </div>
            ) : null}
            {active ? (
              <p className="text-300">
                Power usage and hashrate will change during mapping. Some actions are unavailable until mapping is
                complete. You can close this dialog while mapping continues.
              </p>
            ) : null}
            {unavailable ? (
              <p role="alert" className="text-300 text-text-critical">
                Could not read mapping status: {unavailable}
              </p>
            ) : null}
            {state.notice ? (
              <p role="status" className="text-300">
                {state.notice}
              </p>
            ) : null}
            {unknown ? (
              <p className="text-300">
                The controller did not confirm the previous start. Check the controller and feed states before clearing
                this result. No new run will start automatically.
              </p>
            ) : null}
            {mode === "observe" ? (
              <p className="text-300">
                Observation mode does not interrupt mining. Configure manual mapping or switch-port mapping on the
                controller before you run a correction.
              </p>
            ) : null}
            {mode === "switch_ports" ? (
              <p className="text-300">
                The switch-port schedule sets physical locations. Check the cable connections, CU1 identities, and
                schedule for conflicts. After correction, verify the affected hashboards.
              </p>
            ) : null}
            {state.assessment?.configurationReasons.map((reason) => (
              <p className="text-200" key={reason}>
                {words(reason)}
              </p>
            ))}
            {state.assessment?.deferredReason ? (
              <p className="text-200">Verification is deferred: {words(state.assessment.deferredReason)}.</p>
            ) : null}
            {state.run ? (
              <section aria-label="Mapping progress" className="rounded-xl bg-surface-5 p-4">
                {!active ? <p className="text-emphasis-200">{progressText(state.run)}</p> : null}
                <p className="text-200">
                  {state.run.feeds.filter((feed) => feed.complete).length} of {state.run.feeds.length} feeds checked
                  {state.run.currentFeed ? ` · ${state.run.currentFeed}` : ""}
                </p>
                {state.run.error ? (
                  <p role="alert" className="mt-2 text-200 text-text-critical">
                    {state.run.error}
                  </p>
                ) : null}
                {state.run.warnings.map((warning) => (
                  <p className="mt-2 text-200" key={warning}>
                    {warning}
                  </p>
                ))}
                {state.run.feeds.some((feed) => feed.timedOut) ? (
                  <p className="mt-2 text-200">
                    Some feeds timed out. Check the remaining mapping problems and feed states before you retry.
                  </p>
                ) : null}
                {state.run.status === "failed" || state.run.status === "canceled" ? (
                  <p className="mt-2 text-200">Check feed power states. Restoration may be incomplete.</p>
                ) : null}
              </section>
            ) : null}
            <div className="flex flex-wrap gap-3">
              {state.canVerify && verifyBoards.length ? (
                <Button variant="primary" onClick={() => setVerify(true)}>
                  Verify hashboards
                </Button>
              ) : null}
              {active ? (
                <Button variant="secondary" disabled={!!state.activity} onClick={() => void state.cancel()}>
                  Cancel mapping
                </Button>
              ) : null}
              {unavailable || unknown ? (
                <Button variant="secondary" disabled={!!state.activity} onClick={state.refresh}>
                  Check status
                </Button>
              ) : null}
              {state.run && !state.unresolved && !unavailable ? (
                <Button variant="textOnly" onClick={state.clear}>
                  Dismiss run result
                </Button>
              ) : null}
              {unknown ? (
                <Button variant="textOnly" disabled={!!state.activity} onClick={() => setClearUnknown(true)}>
                  Clear saved attempt
                </Button>
              ) : null}
            </div>
            {boards.length ? (
              <details>
                <summary className="cursor-pointer text-emphasis-200">Hashboard details</summary>
                {(showAll ? boards : affected).length ? (
                  <section aria-label="Hashboard mapping evidence">
                    {(showAll ? boards : affected).map((board) => (
                      <BoardEvidence key={board.mac} board={board} />
                    ))}
                  </section>
                ) : null}
                {boards.length > affected.length ? (
                  <Button variant="textOnly" onClick={() => setShowAll(!showAll)}>
                    {showAll ? "Show affected hashboards" : `Show all ${boards.length} hashboards`}
                  </Button>
                ) : null}
              </details>
            ) : null}
            {state.assessment?.changes.length ? (
              <details>
                <summary className="cursor-pointer text-emphasis-200">Recent mapping changes</summary>
                <ul className="mt-3 space-y-2 text-200">
                  {state.assessment.changes.slice(0, 20).map((change, index) => (
                    <li key={index}>
                      {change.at ? `${new Date(change.at).toLocaleString()} · ` : ""}
                      {change.mac} {change.detail}
                    </li>
                  ))}
                </ul>
              </details>
            ) : null}
            {state.assessment?.historyIncomplete ? (
              <p className="text-200 text-text-primary-70">Mapping history is incomplete.</p>
            ) : null}
          </div>
        </Dialog>
      ) : null}
      {open && verify ? (
        <Dialog
          open
          manageFocus
          title="Verify hashboard wiring?"
          onDismiss={() => setVerify(false)}
          buttons={[
            { text: "Back", variant: variants.secondary, onClick: () => setVerify(false) },
            {
              text: "Queue verification",
              variant: variants.primary,
              disabled: !state.canVerify,
              onClick: async () => {
                await state.verify(verifyBoards.map((board) => board.mac));
                setVerify(false);
              },
            },
          ]}
        >
          <p className="text-300">
            Queue checks for {verifyBoards.length} hashboards. Each check can briefly stop and restart a hashboard.
            Verification does not change the configured physical locations.
          </p>
        </Dialog>
      ) : null}
      {open && clearUnknown ? (
        <Dialog
          open
          manageFocus
          title="Clear the unknown mapping result?"
          onDismiss={() => setClearUnknown(false)}
          buttons={[
            { text: "Back", variant: variants.secondary, onClick: () => setClearUnknown(false) },
            {
              text: "I checked the controller",
              variant: variants.primary,
              onClick: () => {
                state.clear();
                setClearUnknown(false);
              },
            },
          ]}
        >
          <p className="text-300">
            Confirm that you checked the controller and feed power states. Clearing this browser record does not cancel
            a mapping run.
          </p>
        </Dialog>
      ) : null}
    </>
  );
}

export default function ContainerMappingSetup() {
  const state = useFeedMappingState();
  return state ? <MappingSetup state={state} /> : null;
}
