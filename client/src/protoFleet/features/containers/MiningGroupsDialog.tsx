import { useEffect, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { fleetInventoryClient, miningGroupsClient } from "@/protoFleet/api/clients";
import type { GetStateResponse } from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import type { PoolConfig } from "@/protoFleet/api/useMinerCommand";
import PoolSelectionPage from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/PoolSelectionPage";
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
  const targetKey = JSON.stringify([
    props.deviceIdentifier,
    props.moduleIds?.map(String).sort(),
    props.hashboardIds?.map(String).sort(),
  ]);
  // A new target starts a fresh form and cancels reads for the previous target.
  // Sorting preserves the form and retry identity across same-target refreshes.
  return <MiningGroupsForm key={targetKey} {...props} />;
}

function MiningGroupsForm({
  deviceIdentifier,
  moduleIds,
  hashboardIds,
  onClose,
  credentials,
}: MiningGroupsDialogProps) {
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
  const activation = !moduleIds && !hashboardIds;
  const [state, setState] = useState<GetStateResponse>();
  const [boards, setBoards] = useState<bigint[] | undefined>(hashboardIds);
  const resolvedProfile = useRef<{ selection: string; revisionId: string } | undefined>(undefined);
  const [error, setError] = useState("");
  const [observationError, setObservationError] = useState("");
  const [saving, setSaving] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const request = useRef<{ id: string; profile: string; revision: bigint } | undefined>(undefined);
  const refreshState = useRef<(() => Promise<void>) | undefined>(undefined);
  const idsKey = moduleIds?.map(String).sort().join(",");
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
  useEffect(() => {
    if (activation) return;
    const abort = new AbortController();
    if (idsKey)
      void fleetInventoryClient
        .getModuleHashboards({ moduleIds: idsKey.split(",").map(BigInt) }, { signal: abort.signal })
        .then((r) => {
          if (abort.signal.aborted) return;
          if (
            r.modules.length !== idsKey.split(",").length ||
            r.modules.some(
              (m) =>
                m.slots.length !== 3 ||
                m.slots.some(
                  (s) =>
                    !["empty", "occupied"].includes(s.occupancy) ||
                    !!s.unavailableReason ||
                    (s.occupancy === "occupied" && !s.board?.hashboardId),
                ),
            )
          )
            throw new Error("Refresh module inventory before selecting pools.");
          setBoards(
            r.modules.flatMap((m) =>
              m.slots.flatMap((s) => (s.occupancy === "occupied" && s.board ? [s.board.hashboardId] : [])),
            ),
          );
        })
        .catch((e) => {
          if (!abort.signal.aborted) setError(getErrorMessage(e));
        });
    return () => abort.abort();
  }, [activation, idsKey]);
  const pending = state && ["pending", "applying", "blocked"].includes(state.status);
  const fresh = state?.observedAt && now - timestampDate(state.observedAt).getTime() < 120_000;
  const allowed = hasMiningGroupPermissions(permissions, activation ? "activate" : "pools");
  const ready =
    allowed &&
    !!fresh &&
    state?.supported &&
    !pending &&
    !submitted &&
    (activation ? !state.enabled : state.enabled && !!boards?.length && !!credentials);
  const submit = async (profile = "") => {
    if (!ready || saving || !state) return;
    setSaving(true);
    setError("");
    try {
      // Keep the same identity after a lost response. Changing the user's selection
      // is a new request; normal observation polling does not change its revision.
      if (!request.current || request.current.profile !== profile)
        request.current = { id: createOperationID(), profile, revision: state.revision };
      if (activation)
        await miningGroupsClient.activate({
          deviceIdentifier,
          requestId: request.current.id,
          acknowledgeInterruption: true,
        });
      else
        await miningGroupsClient.setPools({
          deviceIdentifier,
          hashboardIds: boards!,
          poolRevisionId: profile,
          requestId: request.current.id,
          expectedRevision: request.current.revision,
          userUsername: credentials!.username,
          userPassword: credentials!.password,
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
  const assignPools = async (config: PoolConfig) => {
    if (!ready || saving) return;
    const slots = [config.defaultPool, config.backup1Pool, config.backup2Pool].filter((slot) => !!slot);
    if (slots.some((slot) => slot.type !== "poolId")) {
      setError("Select saved Fleet pools before assigning hashboards.");
      return;
    }
    const poolIds = slots.map((slot) => BigInt(slot.type === "poolId" ? slot.poolId : "0"));
    const selection = poolIds.map(String).join(",");
    setError("");
    try {
      // Keep the resolved immutable revision after an uncertain submission.
      // Editing a saved pool while retrying must not change accepted intent.
      if (resolvedProfile.current?.selection !== selection) {
        const result = await miningGroupsClient.resolveFleetPools({ poolIds });
        if (!mounted.current) return;
        if (!result.revision) throw new Error("Pool configuration was not returned.");
        resolvedProfile.current = { selection, revisionId: result.revision.revisionId };
      }
      await submit(resolvedProfile.current.revisionId);
    } catch (e) {
      setError(getErrorMessage(e));
    }
  };
  const prerequisite = !allowed
    ? activation
      ? "Enabling mining groups requires permission to enable groups and to start and stop mining."
      : "Changing mining-group pools requires permission to update pools and to start and stop mining."
    : !state
      ? "Loading controller configuration…"
      : !state.supported
        ? "This controller’s firmware does not support mining groups."
        : !fresh
          ? "Controller configuration is stale. Reconnect the controller before making changes."
          : !activation && !state.enabled
            ? "An administrator must enable mining groups on this container first."
            : undefined;
  if (!activation)
    return (
      <PoolSelectionPage
        deviceIdentifiers={[]}
        hashboardTarget={{
          count: boards?.length ?? 0,
          ready: !!ready && !saving,
          submitted,
          message:
            prerequisite ??
            (pending || submitted
              ? state?.message
              : boards
                ? `${boards.length} hashboards selected. They will start mining with this configuration once the change completes.`
                : "Loading hashboards…"),
          error: error || observationError,
        }}
        onAssignPools={assignPools}
        onDismiss={onClose}
      />
    );
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
