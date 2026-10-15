import { useEffect, useRef, useState } from "react";
import { sitesClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { getSiteAssignmentErrorMessage } from "@/protoFleet/features/sites/siteAssignmentErrors";
import { useAuthErrors } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
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
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const busy = useRef(false);
  const { handleAuthErrors } = useAuthErrors();
  const bumpSitesRevision = useFleetStore((state) => state.ui.bumpSitesRevision);
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
        if (!controller.signal.aborted) setError(getErrorMessage(err));
      });
    return () => controller.abort();
  }, []);
  const save = async () => {
    if (busy.current) return;
    busy.current = true;
    setSaving(true);
    setError(undefined);
    try {
      await sitesClient.assignContainersToSite({
        containerIdentifiers: [container.deviceIdentifier],
        targetSiteId: target ? BigInt(target) : undefined,
      });
      bumpSitesRevision();
      pushToast({ message: "Container site updated", status: STATUSES.success });
      onSaved?.();
      onClose();
    } catch (err) {
      handleAuthErrors({ error: err });
      setError(getSiteAssignmentErrorMessage(err));
    } finally {
      busy.current = false;
      setSaving(false);
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
            setError(undefined);
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
