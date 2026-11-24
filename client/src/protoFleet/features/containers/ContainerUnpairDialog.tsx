import { useRef, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { usePermissions } from "@/protoFleet/store";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export default function ContainerUnpairDialog({
  container,
  onClose,
  onUnpaired,
}: {
  container: Container;
  onClose: () => void;
  onUnpaired?: (deviceIdentifier: string) => void;
}) {
  const permissions = usePermissions();
  const allowed = permissions.includes("miner:read") && permissions.includes("miner:delete");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const inFlight = useRef(false);

  const submit = async () => {
    if (inFlight.current || !allowed) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      // Unpair the controller through Fleet, including when its proxy is unavailable.
      await fleetManagementClient.deleteMiners({
        deviceSelector: {
          selectionType: { case: "includeDevices", value: { deviceIdentifiers: [container.deviceIdentifier] } },
        },
      });
      pushToast({ message: `${container.name}: container unpaired.`, status: STATUSES.success });
      onClose();
      onUnpaired?.(container.deviceIdentifier);
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
      title={`Unpair ${container.name}?`}
      subtitle="This removes the container from your fleet. You can pair it again later."
      testId="container-unpair-dialog"
      onDismiss={busy ? undefined : onClose}
      buttons={[
        { text: "Cancel", variant: variants.secondary, onClick: onClose, disabled: busy },
        {
          text: busy ? "Unpairing…" : "Unpair",
          variant: variants.secondaryDanger,
          onClick: () => void submit(),
          disabled: busy || !allowed,
        },
      ]}
    >
      {error ? (
        <p role="alert" className="text-intent-critical-text">
          {error}
        </p>
      ) : null}
      {!allowed ? <p role="alert">This action is no longer available for this container.</p> : null}
    </Dialog>
  );
}
