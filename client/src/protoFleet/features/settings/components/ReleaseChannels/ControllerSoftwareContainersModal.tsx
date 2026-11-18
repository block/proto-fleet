import { useCallback } from "react";
import { Link } from "react-router-dom";

import type { ControllerSoftwareApi } from "./controllerSoftwareUtils";
import { minerLabel, pairLabel } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import { useRefreshingRead } from "./useRefreshingRead";
import type { ReleaseChannelControllerSoftware } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { containerDetailPath } from "@/protoFleet/features/containers/useOpenContainerView";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Modal, { sizes } from "@/shared/components/Modal";

// Fetch all pages only while this model's detail view is open. As in
// ModelMinersModal, retain the last complete read if a later page fails.
const ControllerSoftwareContainersModal = ({
  channelId,
  channelName,
  model,
  targetVersion,
  listContainers,
  minerNames,
  onClose,
}: {
  channelId: bigint;
  channelName: string;
  model: ReleaseChannelControllerSoftware;
  targetVersion: string;
  listContainers: ControllerSoftwareApi["listContainers"];
  minerNames: Record<string, string>;
  onClose: () => void;
}) => {
  const { manufacturer, model: modelName } = model;
  const read = useCallback(
    async (signal: AbortSignal) => ({
      containers: await listContainers(channelId, manufacturer, modelName, signal),
      targetVersion,
    }),
    [channelId, manufacturer, modelName, listContainers, targetVersion],
  );
  const { data, error, isLoading, refresh, cancel } = useRefreshingRead({
    read,
    refreshKey: model,
    trailingRefresh: true,
    errorMessage: "Couldn't load container controller software.",
  });
  const close = () => {
    cancel();
    onClose();
  };

  return (
    <Modal
      open
      size={sizes.large}
      title={`${pairLabel(model)} controller software`}
      description={channelName}
      testId="controller-software-containers-modal"
      onDismiss={close}
      buttons={[{ text: "Done", variant: variants.primary, onClick: close }]}
    >
      {error ? (
        <div role="alert" aria-busy={isLoading} className="mb-4">
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={data === null ? "Couldn't load containers" : "Container details may be out of date"}
            subtitle={data === null ? error : `${error} Showing the last loaded data.`}
            buttonText={isLoading ? "Retrying..." : "Retry"}
            buttonOnClick={() => {
              if (!isLoading) refresh();
            }}
          />
        </div>
      ) : null}
      {data === null ? (
        isLoading ? (
          <p className="py-4 text-200 text-text-primary-50">Loading containers…</p>
        ) : null
      ) : data.containers.length === 0 ? (
        <p className="py-4 text-200 text-text-primary-50">No containers in this model group.</p>
      ) : (
        <>
          {targetVersion !== "" && data.containers.some((container) => !container.onTarget) ? (
            <p className="mb-4 text-200 text-text-primary-70">
              Update controller software from each container&apos;s page.
            </p>
          ) : null}
          <table className="w-full text-left text-200" aria-busy={isLoading}>
            <thead>
              <tr className="text-text-primary-50">
                <th className="py-1.5 pr-4 font-normal">Container</th>
                <th className="py-1.5 pr-4 font-normal">Controller software</th>
                <th className="py-1.5 font-normal">Status</th>
              </tr>
            </thead>
            <tbody className="text-text-primary">
              {data.containers.map((container) => (
                <tr
                  key={container.deviceIdentifier}
                  className="border-t border-border-5"
                  data-testid={`controller-software-container-${container.deviceIdentifier}`}
                >
                  <td className="py-2 pr-4">
                    <Link className="underline" to={containerDetailPath(container)}>
                      {minerLabel(container.deviceIdentifier, minerNames)}
                    </Link>
                  </td>
                  <td className="py-2 pr-4">{container.controllerSoftwareVersion || "Not reported"}</td>
                  <td className="py-2">
                    {targetVersion === "" ? (
                      <span className="text-text-primary-50">—</span>
                    ) : container.onTarget ? (
                      <StatusChip label={`On ${targetVersion}`} tone="success" />
                    ) : (
                      <StatusChip label="Needs update" tone="progress" />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </Modal>
  );
};

export default ControllerSoftwareContainersModal;
