import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { isCurrentObservation } from "./moduleDetail";
import ReturnHashboardsToDefaultDialog from "./ReturnHashboardsToDefaultDialog";
import { miningGroupsClient } from "@/protoFleet/api/clients";
import type { GetStateResponse } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { refreshPoolAssignments } from "@/protoFleet/features/poolConfigurations/assignmentProgress";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { usePermissions } from "@/protoFleet/store";
import { createOperationID } from "@/protoFleet/utils/crypto";
import Modal from "@/shared/components/Modal";

type MiningGroupsDialogProps = {
  deviceIdentifier: string;
  mode?: "activate" | "deactivate";
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  onClose: () => void;
  credentials?: { username: string; password: string };
};

export default function MiningGroupsDialog(props: MiningGroupsDialogProps) {
  if (props.moduleIds || props.hashboardIds)
    return (
      <ConfigurationSelector
        boards={{ moduleIds: props.moduleIds, hashboardIds: props.hashboardIds }}
        onDismiss={props.onClose}
      />
    );
  const targetKey = `${props.deviceIdentifier}:${props.mode ?? "activate"}`;
  // A different container or action starts a fresh form and cancels old reads.
  return <MiningGroupsForm key={targetKey} {...props} />;
}

function MiningGroupsForm({ deviceIdentifier, onClose, mode = "activate" }: MiningGroupsDialogProps) {
  const deactivating = mode === "deactivate";
  const actionLabel = deactivating ? "Disable mining groups" : "Enable mining groups";
  const permissions = usePermissions();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 4000);
    return () => clearInterval(timer);
  }, []);
  const [state, setState] = useState<GetStateResponse>();
  const [error, setError] = useState("");
  const [observationError, setObservationError] = useState("");
  const [saving, setSaving] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [returningToDefault, setReturningToDefault] = useState(false);
  const requestId = useRef<string | undefined>(undefined);
  const refreshState = useRef<(() => Promise<void>) | undefined>(undefined);
  useEffect(() => {
    if (returningToDefault) return;
    let disposed = false;
    let inFlight: AbortController | undefined;
    const refresh = async (replace = false) => {
      if (disposed || (inFlight && !replace)) return;
      // A post-submit refresh supersedes any poll started before the write.
      // Check the signal even when a transport delivers a canceled response.
      inFlight?.abort();
      const abort = new AbortController();
      inFlight = abort;
      try {
        const next = await miningGroupsClient.getState({ deviceIdentifier }, { signal: abort.signal });
        if (!abort.signal.aborted) {
          setState(next);
          setObservationError("");
        }
      } catch (e) {
        if (!abort.signal.aborted) setObservationError(getErrorMessage(e));
      } finally {
        if (inFlight === abort) inFlight = undefined;
      }
    };
    refreshState.current = () => refresh(true);
    void refresh();
    const timer = setInterval(() => void refresh(), 4000);
    return () => {
      disposed = true;
      inFlight?.abort();
      refreshState.current = undefined;
      clearInterval(timer);
    };
  }, [deviceIdentifier, returningToDefault]);
  const pending = state && ["pending", "applying", "blocked"].includes(state.status);
  const fresh = isCurrentObservation(state?.observedAt, now);
  const allowed = hasMiningGroupPermissions(permissions, mode);
  const canResume =
    deactivating &&
    allowed &&
    fresh &&
    !observationError &&
    state?.supported &&
    state.canResumeDeactivation &&
    !!state.operationId;
  const ready =
    allowed &&
    fresh &&
    state?.supported &&
    !pending &&
    !submitted &&
    (deactivating ? state.enabled && !state.deactivationBlockedReason : !state.enabled);
  const submit = async () => {
    if ((!ready && !canResume) || saving || !state) return;
    setSaving(true);
    setError("");
    try {
      // Polling and retries after a lost response retain the same request ID.
      if (canResume) {
        requestId.current = state.operationId;
        setSubmitted(false);
      } else requestId.current ??= createOperationID();
      const changeMode = deactivating ? miningGroupsClient.deactivate : miningGroupsClient.activate;
      await changeMode({
        deviceIdentifier,
        requestId: requestId.current,
        acknowledgeInterruption: true,
      });
    } catch (e) {
      setError(getErrorMessage(e));
      const code = ConnectError.from(e).code;
      if (code === Code.FailedPrecondition || code === Code.Aborted) {
        // These are definitive submission rejections. A transport failure may
        // have committed, so its original request identity must be retained.
        requestId.current = undefined;
        await refreshState.current?.();
      }
      setSaving(false);
      return;
    }
    setSubmitted(true);
    await refreshState.current?.();
    setSaving(false);
  };
  const failed = state?.status === "failed";
  const modeMatches = !!state && state.enabled === (mode === "activate");
  const complete = modeMatches && !pending && !failed;
  const started = submitted || !!pending || (failed && modeMatches);
  let prerequisite: string | undefined;
  if (!allowed) {
    prerequisite = deactivating
      ? "Disabling mining groups requires permission to disable groups and to start and stop mining."
      : "Enabling mining groups requires permission to enable groups and to start and stop mining.";
  } else if (!state) {
    prerequisite = "Loading controller configuration…";
  } else if (!state.supported) {
    prerequisite = "This controller’s firmware does not support mining groups.";
  } else if (!fresh) {
    prerequisite = "Controller configuration is stale. Reconnect the controller before making changes.";
  } else if (deactivating && !started && !complete) {
    prerequisite = state.deactivationBlockedReason;
  }
  useEffect(() => {
    if (complete) refreshPoolAssignments();
  }, [complete]);
  let buttonLabel = actionLabel;
  if (complete) buttonLabel = "Done";
  else if (saving) buttonLabel = "Submitting…";
  else if (canResume) buttonLabel = "Resume disabling";
  else if (started) buttonLabel = "Close";

  let introduction =
    "Fleet will stop mining before it enables mining groups. Fleet keeps existing pool assignments and checks the settings reported by the installed hashboards. When ready, choose Wake default mining group from the Fleet container menu.";
  if (deactivating) introduction = "The container will keep the default group’s pools and mining settings.";
  const disableBlocked = deactivating && !started && !complete && !!prerequisite;
  const canMove = disableBlocked && allowed && fresh && !observationError && state?.canReturnToDefault;

  let progressMessage: string | undefined;
  let progressRole: "alert" | "status" = "status";
  if (failed) {
    progressMessage = state.message;
    progressRole = "alert";
  } else if (complete) {
    progressMessage = deactivating ? "Mining groups disabled." : "Mining groups enabled.";
  } else if (started) {
    progressMessage = state?.message;
    if (!progressMessage) progressMessage = deactivating ? "Disabling mining groups…" : "Enabling mining groups…";
    if (state?.status === "blocked") progressRole = "alert";
  }
  if (returningToDefault)
    return (
      <ReturnHashboardsToDefaultDialog
        deviceIdentifier={deviceIdentifier}
        onClose={() => {
          setReturningToDefault(false);
          void refreshState.current?.();
        }}
        onComplete={() => {
          setReturningToDefault(false);
          void refreshState.current?.();
        }}
      />
    );
  return (
    <Modal
      open
      title={actionLabel}
      dialogLabel={actionLabel}
      manageFocus
      onDismiss={saving ? undefined : onClose}
      buttons={[
        ...(canResume
          ? [
              {
                text: "Close",
                variant: "secondary" as const,
                disabled: saving,
                dismissModalOnClick: false,
                onClick: onClose,
              },
            ]
          : []),
        ...(canMove
          ? [
              {
                text: "Move unallocated hashboards",
                variant: "primary" as const,
                disabled: saving,
                dismissModalOnClick: false,
                onClick: () => setReturningToDefault(true),
              },
            ]
          : []),
        ...(!canMove
          ? [
              {
                text: disableBlocked ? "Close" : buttonLabel,
                variant: disableBlocked ? ("secondary" as const) : ("primary" as const),
                disabled: saving || (!disableBlocked && !complete && !started && !ready),
                dismissModalOnClick: false,
                onClick: () => {
                  if (!canResume && (disableBlocked || complete || started)) onClose();
                  else void submit();
                },
              },
            ]
          : []),
      ]}
    >
      <div className="flex flex-col gap-4">
        {!started && !complete && !disableBlocked ? <p>{introduction}</p> : null}
        {prerequisite ? <p role="status">{prerequisite}</p> : null}
        {progressMessage && !disableBlocked ? <p role={progressRole}>{progressMessage}</p> : null}
        {complete && !deactivating && state?.groups.some((group) => group.id === "default" && !group.mining) ? (
          <p>Choose Wake default mining group from the Fleet container menu.</p>
        ) : null}
        {error || observationError ? (
          <p role="alert" className="text-intent-critical-fill">
            {error || observationError}
          </p>
        ) : null}
      </div>
    </Modal>
  );
}
