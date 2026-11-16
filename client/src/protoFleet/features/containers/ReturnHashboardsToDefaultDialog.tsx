import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { miningGroupsClient } from "@/protoFleet/api/clients";
import type { DefaultReturnReview } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { refreshPoolAssignments } from "@/protoFleet/features/poolConfigurations/assignmentProgress";
import { useFleetStore, usePermissions } from "@/protoFleet/store";
import { createOperationID } from "@/protoFleet/utils/crypto";
import { Circle } from "@/shared/assets/icons";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type Props = {
  deviceIdentifier?: string;
  hashboardIds?: bigint[];
  onClose: () => void;
  onComplete: () => void;
};
type Submission = { requestId: string; operationId?: string; complete?: boolean; failed?: boolean; message?: string };

// Recall and container deactivation use the same review and confirmation.
// Closing this view never undoes recall or cancels an accepted device operation.
export default function ReturnHashboardsToDefaultDialog({
  deviceIdentifier,
  hashboardIds,
  onClose,
  onComplete,
}: Props) {
  const allowed = hasMiningGroupPermissions(usePermissions(), "pools");
  const [reviews, setReviews] = useState<DefaultReturnReview[]>();
  const [submissions, setSubmissions] = useState<Record<string, Submission>>({});
  const submissionsRef = useRef(submissions);
  const [error, setError] = useState("");
  const [readError, setReadError] = useState("");
  const [saving, setSaving] = useState(false);
  const savingRef = useRef(false);
  const [refresh, setRefresh] = useState(0);
  const mounted = useRef(true);
  const generation = useRef(useFleetStore.getState().auth.sessionGeneration);
  const current = () => mounted.current && useFleetStore.getState().auth.sessionGeneration === generation.current;
  const updateSubmissions = (next: Record<string, Submission>) => {
    submissionsRef.current = next;
    setSubmissions(next);
  };
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    const abort = new AbortController();
    let inFlight = false;
    const poll = async () => {
      if (inFlight || savingRef.current || !allowed) return;
      inFlight = true;
      const reviewing = Object.keys(submissionsRef.current).length === 0;
      try {
        const submitted = Object.entries(submissionsRef.current);
        if (submitted.length) {
          const next = { ...submissionsRef.current };
          for (const [identifier, submission] of submitted) {
            if (!submission.operationId || submission.complete || submission.failed) continue;
            const state = await miningGroupsClient.getState({ deviceIdentifier: identifier }, { signal: abort.signal });
            if (abort.signal.aborted || !current()) return;
            if (state.operationId !== submission.operationId) {
              next[identifier] = {
                ...submission,
                failed: true,
                message: "Another change started. Review the remaining hashboards.",
              };
            } else {
              next[identifier] = {
                ...submission,
                complete: state.status === "complete",
                failed: state.status === "failed",
                message: state.message,
              };
            }
          }
          if (!abort.signal.aborted && current()) updateSubmissions(next);
        } else {
          const response = await miningGroupsClient.reviewReturnToDefault(
            { deviceIdentifier, hashboardIds },
            { signal: abort.signal },
          );
          if (!abort.signal.aborted && current() && Object.keys(submissionsRef.current).length === 0)
            setReviews(response.containers);
        }
        if (!abort.signal.aborted && current()) setReadError("");
      } catch (cause) {
        if (!abort.signal.aborted && current() && (!reviewing || Object.keys(submissionsRef.current).length === 0))
          setReadError(getErrorMessage(cause));
      } finally {
        inFlight = false;
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 4000);
    return () => {
      abort.abort();
      window.clearInterval(timer);
    };
  }, [deviceIdentifier, hashboardIds, refresh, allowed]);

  const started = Object.keys(submissions).length > 0;
  const complete =
    reviews !== undefined &&
    (reviews.length === 0 || (started && reviews.every((review) => submissions[review.deviceIdentifier]?.complete)));
  const failed = Object.values(submissions).some((submission) => submission.failed);
  const pending = Object.values(submissions).some(
    (submission) => submission.operationId && !submission.complete && !submission.failed,
  );
  const ready =
    allowed &&
    !!reviews?.length &&
    reviews.every((review) => review.reviewToken && !review.blockedReason) &&
    !readError;
  const retry = () => {
    updateSubmissions({});
    setReviews(undefined);
    setError("");
    setRefresh((value) => value + 1);
  };
  const submit = async () => {
    if (!allowed || !ready || savingRef.current || !reviews) return;
    savingRef.current = true;
    setSaving(true);
    setError("");
    try {
      for (const review of reviews) {
        if (!current()) return;
        let submission = submissionsRef.current[review.deviceIdentifier];
        if (submission?.operationId) continue;
        submission ??= { requestId: createOperationID() };
        updateSubmissions({ ...submissionsRef.current, [review.deviceIdentifier]: submission });
        try {
          const result = await miningGroupsClient.returnToDefault({
            deviceIdentifier: review.deviceIdentifier,
            hashboardIds: review.boards.map((board) => board.hashboardId),
            reviewToken: review.reviewToken,
            requestId: submission.requestId,
            acknowledgeMiningChange: true,
          });
          if (!current()) return;
          updateSubmissions({
            ...submissionsRef.current,
            [review.deviceIdentifier]: { ...submission, operationId: result.operationId },
          });
        } catch (cause) {
          if (!current()) return;
          const code = ConnectError.from(cause).code;
          if ([Code.FailedPrecondition, Code.Aborted, Code.PermissionDenied, Code.InvalidArgument].includes(code)) {
            updateSubmissions({
              ...submissionsRef.current,
              [review.deviceIdentifier]: { ...submission, failed: true, message: getErrorMessage(cause) },
            });
          }
          // A lost reply retains the request ID and reviewed payload for retry.
          throw cause;
        }
      }
    } catch (cause) {
      if (current()) setError(getErrorMessage(cause));
    } finally {
      savingRef.current = false;
      if (current()) {
        setSaving(false);
        setRefresh((value) => value + 1);
      }
    }
  };
  let action = "Move hashboards";
  if (complete) action = "Done";
  else if (saving) action = "Submitting…";
  else if (failed && !pending) action = "Review hashboards";
  else if (pending) action = "Close";

  if (!started && !complete && !ready) {
    const title = allowed ? "Preparing hashboards" : "Cannot move hashboards";
    const blockers = [...new Set(reviews?.map((review) => review.blockedReason).filter(Boolean))];
    return (
      <Modal
        key="preparation"
        open
        title={title}
        dialogLabel={title}
        manageFocus
        onDismiss={onClose}
        buttons={[
          { text: "Close", variant: "secondary", dismissModalOnClick: false, onClick: onClose },
          ...(allowed && readError
            ? [
                {
                  text: "Try again",
                  variant: "secondary" as const,
                  dismissModalOnClick: false,
                  onClick: () => setRefresh((value) => value + 1),
                },
              ]
            : []),
          ...(allowed
            ? [{ text: "Move hashboards", variant: "primary" as const, disabled: true, dismissModalOnClick: false }]
            : []),
        ]}
      >
        <div className="flex flex-col gap-4">
          {allowed ? (
            <>
              <ol aria-label="Preparation steps" className="flex flex-col gap-4">
                <li>
                  <div role="status" className="flex items-center gap-2">
                    <span aria-hidden="true" className="shrink-0">
                      {readError ? (
                        <Circle width="w-5" className="text-text-primary-50" />
                      ) : (
                        <ProgressCircular indeterminate size={20} />
                      )}
                    </span>
                    <span>Finish pending changes</span>
                  </div>
                  {blockers.map((reason) => (
                    <p key={reason} className="mt-2 pl-7 text-text-primary-50">
                      {reason}
                    </p>
                  ))}
                </li>
                <li className="flex items-center gap-2 text-text-primary-50">
                  <span aria-hidden="true">
                    <Circle width="w-5" className="text-text-primary-50" />
                  </span>
                  <span>Check hashboards and default group settings</span>
                </li>
              </ol>
              <p>The move confirmation will open when these steps finish.</p>
            </>
          ) : (
            <p role="alert">You need permission to change pools and to start and stop mining.</p>
          )}
          {readError ? (
            <p role="alert" className="text-intent-critical-fill">
              {readError}
            </p>
          ) : null}
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      key="confirmation"
      open
      title="Move hashboards to default group?"
      dialogLabel="Move hashboards to default group?"
      manageFocus
      onDismiss={saving ? undefined : onClose}
      buttons={[
        ...(!pending && !complete
          ? [
              {
                text: "Cancel",
                variant: "secondary" as const,
                disabled: saving,
                dismissModalOnClick: false,
                onClick: onClose,
              },
            ]
          : []),
        {
          text: action,
          variant: "primary",
          disabled: saving || (!complete && !pending && !failed && !ready),
          dismissModalOnClick: false,
          onClick: () => {
            if (complete) {
              refreshPoolAssignments();
              onComplete();
            } else if (pending) onClose();
            else if (failed) retry();
            else void submit();
          },
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        {!started && !complete ? (
          <p>
            If you move these hashboards, they will use the default group’s pools and mining settings. Mining can resume
            after the move if the group is running.
          </p>
        ) : null}
        {reviews?.map((review) => {
          const submission = submissions[review.deviceIdentifier];
          const message = submission?.message || (submission ? "Moving hashboards…" : "");
          return (
            <div key={review.deviceIdentifier} className="flex flex-col gap-2">
              {reviews.length > 1 ? <p>{review.deviceIdentifier}</p> : null}
              {review.boards.length > 1 ? <p>{review.boards.length.toLocaleString()} hashboards selected</p> : null}
              <ul aria-label="Selected hashboards" className="max-h-40 list-disc overflow-y-auto pl-5">
                {review.boards.map((board) => (
                  <li key={String(board.hashboardId)} className="break-all">
                    {board.name || `Hashboard ${board.hashboardId}`}
                  </li>
                ))}
              </ul>
              {!started && review.defaultGroup ? (
                <>
                  <p>Mining: {review.defaultGroup.mining ? "Running" : "Stopped"}</p>
                  {review.defaultGroup.pools.length ? (
                    <ul className="list-disc pl-5">
                      {review.defaultGroup.pools.map((pool) => (
                        <li key={pool.id}>
                          {pool.url} · {pool.username}
                          {pool.enabled ? "" : " (disabled)"}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p>The default group has no pools.</p>
                  )}
                </>
              ) : null}
              {message ? <p role={submission?.failed ? "alert" : "status"}>{message}</p> : null}
            </div>
          );
        })}
        {complete ? <p role="status">All selected hashboards are in the default mining group.</p> : null}
        {error || readError ? (
          <p role="alert" className="text-intent-critical-fill">
            {error || readError}
          </p>
        ) : null}
      </div>
    </Modal>
  );
}
