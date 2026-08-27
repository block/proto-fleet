import { useMemo } from "react";

import { deviceStateLabels, deviceStateTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type Rollout,
  RolloutDeviceState,
  type RolloutLaneModelGroup,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";

interface ModelMinersModalProps {
  laneName: string;
  group: RolloutLaneModelGroup;
  activeRollout: Rollout | undefined;
  minerNames: Record<string, string>;
  onClose: () => void;
}

// Per-model miner table, shown on demand so lane cards stay compact no
// matter how many miners a model group holds. Data flows from the polled
// lane, so firmware versions and rollout states update live while open.
const ModelMinersModal = ({ laneName, group, activeRollout, minerNames, onClose }: ModelMinersModalProps) => {
  const deviceStates = useMemo(() => {
    const states: Record<string, RolloutDeviceState> = {};
    for (const device of activeRollout?.devices ?? []) {
      states[device.deviceIdentifier] = device.state;
    }
    return states;
  }, [activeRollout]);

  return (
    <Modal
      open
      size={sizes.large}
      title={`${group.model || "Unknown model"} miners`}
      description={laneName}
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
            const state = deviceStates[miner.deviceIdentifier];
            const onTarget = group.firmwareVersion !== "" && miner.firmwareVersion === group.firmwareVersion;
            return (
              <tr
                key={miner.deviceIdentifier}
                className="border-t border-border-5"
                data-testid={`lane-miner-${miner.deviceIdentifier}`}
              >
                <td className="py-2 pr-4">{minerNames[miner.deviceIdentifier] || miner.deviceIdentifier}</td>
                <td className="py-2 pr-4">{miner.firmwareVersion || "Unknown"}</td>
                <td className="py-2">
                  {state !== undefined && state !== RolloutDeviceState.UNSPECIFIED ? (
                    <StatusChip label={deviceStateLabels[state]} tone={deviceStateTone(state)} />
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
