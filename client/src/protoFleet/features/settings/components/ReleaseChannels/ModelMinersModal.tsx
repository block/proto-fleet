import { useEffect, useMemo, useState } from "react";

import { phaseLabels, phaseTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type ReleaseChannelMiner,
  type ReleaseChannelModelGroup,
  type Rollout,
  type RolloutDevice,
  RolloutDevicePhase,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";

interface ModelMinersModalProps {
  channelId: bigint;
  channelName: string;
  group: ReleaseChannelModelGroup;
  activeRollout: Rollout | undefined;
  minerNames: Record<string, string>;
  listChannelMiners: (channelId: bigint, model?: string) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint) => Promise<RolloutDevice[]>;
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
  const [miners, setMiners] = useState<ReleaseChannelMiner[] | null>(null);
  const [devices, setDevices] = useState<RolloutDevice[]>([]);

  const activeRolloutId = activeRollout?.id;
  useEffect(() => {
    let cancelled = false;
    Promise.all([
      listChannelMiners(channelId, group.model),
      activeRolloutId ? listRolloutDevices(activeRolloutId) : Promise.resolve<RolloutDevice[]>([]),
    ])
      .then(([nextMiners, nextDevices]) => {
        if (cancelled) return;
        setMiners(nextMiners);
        setDevices(nextDevices);
      })
      .catch(() => {
        if (!cancelled) setMiners((current) => current ?? []);
      });
    return () => {
      cancelled = true;
    };
    // `group` and `activeRollout` are new objects on every poll; refetching
    // on them is what keeps the table live.
  }, [channelId, group, activeRollout, activeRolloutId, listChannelMiners, listRolloutDevices]);

  const phases = useMemo(() => {
    const byIdentifier: Record<string, RolloutDevicePhase> = {};
    for (const device of devices) {
      byIdentifier[device.deviceIdentifier] = device.phase;
    }
    return byIdentifier;
  }, [devices]);

  return (
    <Modal
      open
      size={sizes.large}
      title={`${group.model || "Unknown model"} miners`}
      description={channelName}
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      {miners === null ? (
        <p className="py-4 text-200 text-text-primary-50" data-testid="channel-miners-loading">
          Loading miners…
        </p>
      ) : (
        <table className="w-full text-left text-200">
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
              const onTarget = group.firmwareVersion !== "" && miner.firmwareVersion === group.firmwareVersion;
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
