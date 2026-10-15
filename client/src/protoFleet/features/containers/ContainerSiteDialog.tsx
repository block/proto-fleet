import { useEffect, useState } from "react";
import { sitesClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAssignContainersToSite } from "@/protoFleet/features/sites/hooks/useAssignContainersToSite";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export default function ContainerSiteDialog({
  container,
  onClose,
  onSaved,
}: {
  container: Container;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [target, setTarget] = useState(container.site?.id.toString() ?? "");
  const [options, setOptions] = useState<{ value: string; label: string }[]>();
  const [loadError, setLoadError] = useState<string>();
  const { assign, error: assignmentError, saving, clearError } = useAssignContainersToSite();
  const error = assignmentError ?? loadError;
  useEffect(() => {
    const controller = new AbortController();
    void sitesClient
      .listSites({}, { signal: controller.signal })
      .then(({ sites }) => {
        if (!controller.signal.aborted)
          setOptions([
            { value: "", label: "Unassigned" },
            ...sites.flatMap(({ site }) => (site ? [{ value: site.id.toString(), label: site.name }] : [])),
          ]);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setLoadError(getErrorMessage(err));
      });
    return () => controller.abort();
  }, []);
  const save = async () => {
    if (await assign([container.deviceIdentifier], target ? BigInt(target) : undefined)) {
      pushToast({ message: "Container site updated", status: STATUSES.success });
      onSaved?.();
      onClose();
    }
  };
  return (
    <Modal
      open
      title="Assign container to site"
      onDismiss={saving ? undefined : onClose}
      buttons={[
        {
          text: saving ? "Saving…" : "Save",
          variant: "primary",
          onClick: () => void save(),
          disabled: saving || !options || target === (container.site?.id.toString() ?? ""),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <p>{container.name || container.deviceIdentifier}</p>
        <Select
          id="container-site"
          label="Site"
          value={target}
          options={options ?? []}
          onChange={(value) => {
            setTarget(value);
            clearError();
          }}
          disabled={saving || !options}
        />
        <p className="text-200 text-text-primary-70">
          All tanks and modules move with this container. Access follows the new site.
        </p>
        {error ? (
          <div role="alert">
            <Callout intent="danger" prefixIcon={<Alert />} title="Couldn't change site" subtitle={error} />
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
