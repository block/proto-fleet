import { useEffect, useRef, useState } from "react";
import { type ContainerAction, type ContainerActionResponse, requestContainerAction } from "./containerActions";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { usePermissions } from "@/protoFleet/store";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export default function ContainerActionDialog({
  container,
  action,
  miningScope,
  onClose,
}: {
  container: Container;
  action: ContainerAction;
  miningScope: "container" | "default-group" | "unknown";
  onClose: () => void;
}) {
  const permissions = usePermissions();
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState<string>();
  const [target, setTarget] = useState<ContainerActionResponse>();
  const [watts, setWatts] = useState("");
  const isPower = action.kind === "power";
  const allowed = permissions.includes(action.permission) && permissions.includes("miner:read");

  useEffect(() => {
    if (!isPower || !allowed) return;
    const abort = new AbortController();
    void requestContainerAction(container.deviceIdentifier, "mining/target-power", "GET", undefined, abort.signal)
      .then((data) => {
        if (abort.signal.aborted) return;
        setTarget(data);
        setWatts(String(data.watts ?? 0));
      })
      .catch((cause) => {
        if (!abort.signal.aborted) setError(getErrorMessage(cause));
      });
    return () => abort.abort();
  }, [container.deviceIdentifier, isPower, allowed]);

  const value = Number(watts);
  // Zero clears the controller's target instead of setting a power limit.
  const min = Math.max(1, typeof target?.minimumWatts === "number" ? target.minimumWatts : 1);
  const max = typeof target?.maximumWatts === "number" && target.maximumWatts > 0 ? target.maximumWatts : undefined;
  const validPower =
    watts.trim() !== "" && Number.isFinite(value) && value >= min && (max === undefined || value <= max);
  const disabled = busy || !allowed || (isPower && (!target || !validPower));

  const submit = async () => {
    if (inFlight.current || disabled) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const body = isPower
        ? {
            watts: value,
            performanceMode: target?.performanceMode,
            balanceBays: target?.balanceBays,
            hashOnDisconnect: target?.hashOnDisconnect,
          }
        : {};
      const result = await requestContainerAction(
        container.deviceIdentifier,
        action.path,
        isPower ? "PUT" : "POST",
        body,
      );
      pushToast({
        message:
          isPower && typeof result.watts === "number"
            ? `${container.name}: power target set to ${result.watts.toLocaleString()} W.`
            : `${container.name}: ${action.label.toLowerCase()} request accepted.`,
        status: STATUSES.success,
      });
      onClose();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      title={`${action.label}: ${container.name}`}
      subtitle={
        action.path === "mining/start" || action.path === "mining/stop"
          ? miningScope === "default-group"
            ? "Applies only to the default mining group. Other mining groups are unaffected."
            : miningScope === "container"
              ? "Applies to the whole container. Isolated boards keep their individual mining state."
              : "If mining groups are enabled, this affects only the default mining group."
          : "Applies to this container. The controller determines whether the command can be applied."
      }
      onDismiss={busy ? undefined : onClose}
      testId="container-action-dialog"
      buttons={[
        { text: "Cancel", variant: variants.secondary, onClick: onClose, disabled: busy },
        {
          text: busy ? "Sending…" : "Confirm",
          variant: variants.primary,
          onClick: () => void submit(),
          disabled,
        },
      ]}
    >
      {isPower ? (
        <>
          <Input
            id="container-power-target"
            label="Container power target"
            units="W"
            type="number"
            initValue={watts}
            onChange={setWatts}
            disabled={busy || !target}
          />
          <p className="mt-2 text-200 text-text-primary-70">
            {target
              ? `Minimum ${min.toLocaleString()} W${max === undefined ? "" : `; maximum ${max.toLocaleString()} W`}.`
              : "Loading power target…"}
          </p>
        </>
      ) : null}
      {error ? (
        <p role="alert" className="mt-3 text-intent-critical-text">
          {error}
        </p>
      ) : null}
      {!allowed ? <p role="alert">This action is no longer available for this container.</p> : null}
    </Dialog>
  );
}
