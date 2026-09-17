import { useEffect, useMemo, useState } from "react";

import { pairLabel, phaseLabels, phaseTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
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
  listChannelMiners: (channelId: bigint, manufacturer?: string, model?: string) => Promise<ReleaseChannelMiner[]>;
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
  const activeRolloutId = activeRollout?.id;
  const context = JSON.stringify([channelId.toString(), group.manufacturer, group.model, activeRolloutId?.toString()]);
  const [retryAttempt, setRetryAttempt] = useState(0);
  const request = useMemo(
    () => ({ channelId, group, activeRollout, listChannelMiners, listRolloutDevices, retryAttempt }),
    [channelId, group, activeRollout, listChannelMiners, listRolloutDevices, retryAttempt],
  );
  const [snapshot, setSnapshot] = useState({
    context,
    request: null as object | null,
    miners: null as ReleaseChannelMiner[] | null,
    devices: [] as RolloutDevice[],
    error: null as string | null,
  });
  useEffect(() => {
    let cancelled = false;
    Promise.all([
      request.listChannelMiners(request.channelId, request.group.manufacturer, request.group.model),
      request.activeRollout
        ? request.listRolloutDevices(request.activeRollout.id)
        : Promise.resolve<RolloutDevice[]>([]),
    ])
      .then(([nextMiners, nextDevices]) => {
        if (cancelled) return;
        // Empty identities mean "unknown" in model groups but wildcard in the
        // list request. Narrow the fully loaded result to this exact raw pair.
        const groupMiners =
          request.group.manufacturer === "" || request.group.model === ""
            ? nextMiners.filter(
                (miner) => miner.manufacturer === request.group.manufacturer && miner.model === request.group.model,
              )
            : nextMiners;
        setSnapshot({ context, request, miners: groupMiners, devices: nextDevices, error: null });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setSnapshot((previous) => ({
          ...(previous.context === context ? previous : { context, miners: null, devices: [] }),
          request,
          error: error instanceof Error && error.message ? error.message : "The request failed. Try again.",
        }));
      });
    return () => {
      cancelled = true;
    };
    // `group` and `activeRollout` are new objects on every poll; refetching
    // on them is what keeps the table live.
  }, [context, request]);

  // Do not display the preceding model's data under a new context, even in the
  // render before its request settles. Publish both lists together.
  const current = snapshot.context === context ? snapshot : undefined;
  const miners = current?.miners ?? null;
  const isLoading = current?.request !== request;
  const error = current?.error;

  const phases = useMemo(() => {
    const byIdentifier: Record<string, RolloutDevicePhase> = {};
    for (const device of current?.devices ?? []) {
      byIdentifier[device.deviceIdentifier] = device.phase;
    }
    return byIdentifier;
  }, [current?.devices]);

  return (
    <Modal
      open
      size={sizes.large}
      title={`${pairLabel(group)} miners`}
      description={channelName}
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
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
              if (!isLoading) setRetryAttempt((attempt) => attempt + 1);
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
