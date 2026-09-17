import { useCallback, useMemo } from "react";

import { pairLabel, phaseLabels, phaseTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import { useRefreshingRead } from "./useRefreshingRead";
import {
  type ReleaseChannelMiner,
  type ReleaseChannelModelGroup,
  type Rollout,
  type RolloutDevice,
  RolloutDevicePhase,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Modal, { sizes } from "@/shared/components/Modal";

interface ModelMinersModalProps {
  channelId: bigint;
  channelName: string;
  group: ReleaseChannelModelGroup;
  activeRollout: Rollout | undefined;
  minerNames: Record<string, string>;
  listChannelMiners: (
    channelId: bigint,
    manufacturer?: string,
    model?: string,
    signal?: AbortSignal,
  ) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint, signal?: AbortSignal) => Promise<RolloutDevice[]>;
  onClose: () => void;
}

// Per-model miner table, shown on demand so the manage view stays compact no
// matter how many miners a model group holds. The server pages members and
// rollout devices separately from the channel summary, so this fetches both
// when opened and again whenever the polled summary changes, keeping firmware
// versions and phases live while open.
const ModelMinersModal = ({
  channelId,
  channelName,
  group,
  activeRollout,
  minerNames,
  listChannelMiners,
  listRolloutDevices,
  onClose,
}: ModelMinersModalProps) => {
  const activeRolloutId = activeRollout?.id;
  const { manufacturer, model } = group;
  const read = useCallback(
    async (signal: AbortSignal) => {
      // Drain both branches before releasing the scan, including when one fails.
      const [members, progress] = await Promise.allSettled([
        listChannelMiners(channelId, manufacturer, model, signal),
        activeRolloutId !== undefined
          ? listRolloutDevices(activeRolloutId, signal)
          : Promise.resolve<RolloutDevice[]>([]),
      ]);
      if (members.status === "rejected") throw members.reason;
      if (progress.status === "rejected") throw progress.reason;
      // Empty observed identities are wildcard filters in the API. Narrow the
      // complete response back to the raw model group being displayed.
      const miners =
        manufacturer === "" || model === ""
          ? members.value.filter((miner) => miner.manufacturer === manufacturer && miner.model === model)
          : members.value;
      return { miners, devices: progress.value };
    },
    [channelId, manufacturer, model, activeRolloutId, listChannelMiners, listRolloutDevices],
  );
  const refreshKey = useMemo(() => ({ group, activeRollout }), [group, activeRollout]);
  const { data, isLoading, error, refresh, cancel } = useRefreshingRead({
    read,
    refreshKey,
    trailingRefresh: true,
    errorMessage: "The request failed. Try again.",
  });
  const miners = data?.miners ?? null;

  const phases = useMemo(() => {
    const byIdentifier: Record<string, RolloutDevicePhase> = {};
    for (const device of data?.devices ?? []) {
      byIdentifier[device.deviceIdentifier] = device.phase;
    }
    return byIdentifier;
  }, [data?.devices]);

  const handleClose = () => {
    cancel();
    onClose();
  };

  return (
    <Modal
      open
      size={sizes.large}
      title={`${pairLabel(group)} miners`}
      description={channelName}
      onDismiss={handleClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: handleClose }]}
    >
      {error ? (
        <div role="alert" aria-busy={isLoading} className="mb-4">
          <Callout
            intent={intents.warning}
            prefixIcon={<Alert />}
            title={miners === null ? "Couldn't load miners" : "Miner details may be out of date"}
            subtitle={miners === null ? error : `${error} Showing the last loaded data.`}
            buttonText={isLoading ? "Retrying..." : "Retry"}
            buttonOnClick={() => {
              if (!isLoading) refresh();
            }}
          />
        </div>
      ) : null}
      {miners === null ? (
        isLoading ? (
          <p className="py-4 text-200 text-text-primary-50" data-testid="channel-miners-loading">
            Loading miners…
          </p>
        ) : null
      ) : miners.length === 0 ? (
        <p className="py-4 text-200 text-text-primary-50">No miners in this model group.</p>
      ) : (
        <table className="w-full text-left text-200" aria-busy={isLoading}>
          <thead>
            <tr className="text-text-primary-50">
              <th className="py-1.5 pr-4 font-normal">Miner</th>
              <th className="py-1.5 pr-4 font-normal">Current firmware</th>
              <th className="py-1.5 font-normal">Status</th>
            </tr>
          </thead>
          <tbody className="text-text-primary">
            {miners.map((miner) => {
              const phase = phases[miner.deviceIdentifier];
              // A version string can describe different firmware artifacts.
              // Match the server's assignment count using deployment provenance.
              const onTarget =
                group.firmwareVersion !== "" &&
                group.firmwareChecksum !== "" &&
                miner.firmwareVersion === group.firmwareVersion &&
                miner.lastDeployedFirmwareChecksum === group.firmwareChecksum;
              return (
                <tr
                  key={miner.deviceIdentifier}
                  className="border-t border-border-5"
                  data-testid={`channel-miner-${miner.deviceIdentifier}`}
                >
                  <td className="py-2 pr-4">
                    {minerNames[miner.deviceIdentifier] || miner.deviceIdentifier}
                    {miner.conflicted ? (
                      <span
                        className="ml-2 text-text-primary-50"
                        title="Another channel's scope also covers this miner"
                      >
                        (also in another channel)
                      </span>
                    ) : null}
                  </td>
                  <td className="py-2 pr-4">{miner.firmwareVersion || "Unknown"}</td>
                  <td className="py-2">
                    {phase !== undefined && phase !== RolloutDevicePhase.UNSPECIFIED ? (
                      <StatusChip label={phaseLabels[phase]} tone={phaseTone(phase)} />
                    ) : onTarget ? (
                      <StatusChip label="On assigned version" tone="success" />
                    ) : group.firmwareVersion !== "" ? (
                      <StatusChip label="Not on assigned version" tone="neutral" />
                    ) : (
                      <span className="text-text-primary-50">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Modal>
  );
};

export default ModelMinersModal;
