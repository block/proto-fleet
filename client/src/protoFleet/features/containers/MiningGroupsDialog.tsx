import { useEffect, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { miningGroupsClient } from "@/protoFleet/api/clients";
import type { GetStateResponse } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { usePermissions } from "@/protoFleet/store";
import { createOperationID } from "@/protoFleet/utils/crypto";
import Modal from "@/shared/components/Modal";

type MiningGroupsDialogProps = {
  deviceIdentifier: string;
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
  const targetKey = props.deviceIdentifier;
  // A new target starts a fresh form and cancels reads for the previous target.
  // Sorting preserves the form and retry identity across same-target refreshes.
  return <MiningGroupsForm key={targetKey} {...props} />;
}

function MiningGroupsForm({ deviceIdentifier, onClose }: MiningGroupsDialogProps) {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
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
  const request = useRef<{ id: string; profile: string; revision: bigint } | undefined>(undefined);
  const refreshState = useRef<(() => Promise<void>) | undefined>(undefined);
  useEffect(() => {
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
  }, [deviceIdentifier]);
  const pending = state && ["pending", "applying", "blocked"].includes(state.status);
  const fresh = state?.observedAt && now - timestampDate(state.observedAt).getTime() < 120_000;
  const allowed = hasMiningGroupPermissions(permissions, "activate");
  const ready = allowed && !!fresh && state?.supported && !pending && !submitted && !state.enabled;
  const submit = async (profile = "") => {
    if (!ready || saving || !state) return;
    setSaving(true);
    setError("");
    try {
      // Keep the same identity after a lost response. Changing the user's selection
      // is a new request; normal observation polling does not change its revision.
      if (!request.current || request.current.profile !== profile)
        request.current = { id: createOperationID(), profile, revision: state.revision };
      await miningGroupsClient.activate({
        deviceIdentifier,
        requestId: request.current.id,
        acknowledgeInterruption: true,
      });
    } catch (e) {
      setError(getErrorMessage(e));
      const code = ConnectError.from(e).code;
      if (code === Code.FailedPrecondition || code === Code.Aborted) {
        // These are definitive submission rejections. A transport failure may
        // have committed, so its original request identity must be retained.
        request.current = undefined;
        await refreshState.current?.();
      }
      setSaving(false);
      return;
    }
    setSubmitted(true);
    await refreshState.current?.();
    setSaving(false);
  };
  const prerequisite = !allowed
    ? "Enabling mining groups requires permission to enable groups and to start and stop mining."
    : !state
      ? "Loading controller configuration…"
      : !state.supported
        ? "This controller’s firmware does not support mining groups."
        : !fresh
          ? "Controller configuration is stale. Reconnect the controller before making changes."
          : undefined;
  const activationFailed = state?.status === "failed";
  const activationComplete = !!state?.enabled && !pending && !activationFailed;
  const activationStarted = submitted || !!pending || (activationFailed && !!state?.enabled);
  return (
    <Modal
      open
      title="Enable mining groups"
      onDismiss={saving ? undefined : onClose}
      buttons={[
        {
          text: activationComplete
            ? "Done"
            : saving
              ? "Submitting…"
              : activationStarted
                ? "Close"
                : "Enable mining groups",
          variant: "primary",
          disabled: saving || (!activationComplete && !activationStarted && !ready),
          dismissModalOnClick: false,
          onClick: () => {
            if (activationComplete || activationStarted) onClose();
            else void submit();
          },
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        {!activationStarted && !activationComplete ? (
          <p>
            Fleet will pause mining to enable mining groups, then resume with the existing pools if this container was
            running. A stopped container will stay stopped.
          </p>
        ) : null}
        {prerequisite ? <p role="status">{prerequisite}</p> : null}
        {activationFailed ? (
          <p role="alert">{state.message}</p>
        ) : activationComplete ? (
          <p role="status">Mining groups enabled.</p>
        ) : activationStarted ? (
          <p role={state?.status === "blocked" ? "alert" : "status"}>{state?.message || "Enabling mining groups…"}</p>
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
