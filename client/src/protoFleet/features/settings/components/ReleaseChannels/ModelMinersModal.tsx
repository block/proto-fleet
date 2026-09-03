import { useMemo } from "react";

import { phaseLabels, phaseTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type ReleaseChannelModelGroup,
  type Rollout,
  RolloutDevicePhase,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";

interface ModelMinersModalProps {
  channelName: string;
  group: ReleaseChannelModelGroup;
  activeRollout: Rollout | undefined;
  minerNames: Record<string, string>;
  onClose: () => void;
}

// Per-model miner table, shown on demand so the manage view stays compact no
// matter how many miners a model group holds. Data flows from the polled
// channel, so firmware versions and phases update live while open.
const ModelMinersModal = ({ channelName, group, activeRollout, minerNames, onClose }: ModelMinersModalProps) => {
  const phases = useMemo(() => {
    const byIdentifier: Record<string, RolloutDevicePhase> = {};
    for (const device of activeRollout?.devices ?? []) {
      byIdentifier[device.deviceIdentifier] = device.phase;
    }
    return byIdentifier;
  }, [activeRollout]);

  return (
    <Modal
      open
      size={sizes.large}
      title={`${group.model || "Unknown model"} miners`}
      description={channelName}
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      <table className="w-full text-left text-200">
        <thead>
          <tr className="text-text-primary-50">
            <th className="py-1.5 pr-4 font-normal">Miner</th>
            <th className="py-1.5 pr-4 font-normal">Current firmware</th>
            <th className="py-1.5 font-normal">Status</th>
          </tr>
        </thead>
        <tbody className="text-text-primary">
          {group.miners.map((miner) => {
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
                    <span className="ml-2 text-text-primary-50" title="Another channel's scope also covers this miner">
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
    </Modal>
  );
};

export default ModelMinersModal;
