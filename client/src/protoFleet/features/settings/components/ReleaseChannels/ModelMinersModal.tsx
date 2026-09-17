import { useEffect, useMemo, useRef, useState } from "react";

import { pairLabel, phaseLabels, phaseTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type ReleaseChannelMiner,
  type ReleaseChannelModelGroup,
  type Rollout,
  type RolloutDevice,
  RolloutDevicePhase,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFleetStore, useIsAuthenticated, useSessionGeneration, useUsername } from "@/protoFleet/store";
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
  const username = useUsername();
  const sessionGeneration = useSessionGeneration();
  const isAuthenticated = useIsAuthenticated();
  const context = useMemo(
    () => ({
      channelId,
      manufacturer: group.manufacturer,
      model: group.model,
      activeRolloutId,
      listChannelMiners,
      listRolloutDevices,
      username,
      sessionGeneration,
      isAuthenticated,
    }),
    [
      channelId,
      group.manufacturer,
      group.model,
      activeRolloutId,
      listChannelMiners,
      listRolloutDevices,
      username,
      sessionGeneration,
      isAuthenticated,
    ],
  );
  const refreshRef = useRef<(() => void) | null>(null);
  const [snapshot, setSnapshot] = useState({
    context,
    isLoading: true,
    miners: null as ReleaseChannelMiner[] | null,
    devices: [] as RolloutDevice[],
    error: null as string | null,
  });
  useEffect(() => {
    let cancelled = false;
    let running = false;
    let queued = false;
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        !cancelled &&
        auth.username === context.username &&
        auth.sessionGeneration === context.sessionGeneration &&
        auth.isAuthenticated === context.isAuthenticated
      );
    };
    const refresh = () => {
      if (!isCurrent()) return;
      if (running) {
        queued = true;
        return;
      }
      running = true;
      setSnapshot((previous) => ({
        ...(previous.context === context ? previous : { context, miners: null, devices: [], error: null }),
        isLoading: true,
      }));
      // A failed list must not release the scan while the other paginated
      // list is still running. Publish only a complete pair of results.
      void Promise.allSettled([
        context.listChannelMiners(context.channelId, context.manufacturer, context.model),
        context.activeRolloutId !== undefined
          ? context.listRolloutDevices(context.activeRolloutId)
          : Promise.resolve<RolloutDevice[]>([]),
      ]).then(([members, progress]) => {
        if (!isCurrent()) return;
        if (members.status === "fulfilled" && progress.status === "fulfilled") {
          // Empty identities mean "unknown" in model groups but wildcard in
          // the list request. Narrow the complete result to this raw pair.
          const miners =
            context.manufacturer === "" || context.model === ""
              ? members.value.filter(
                  (miner) => miner.manufacturer === context.manufacturer && miner.model === context.model,
                )
              : members.value;
          setSnapshot({ context, isLoading: false, miners, devices: progress.value, error: null });
        } else {
          const error: unknown =
            members.status === "rejected"
              ? members.reason
              : progress.status === "rejected"
                ? progress.reason
                : undefined;
          setSnapshot((previous) => ({
            ...previous,
            isLoading: false,
            error: error instanceof Error && error.message ? error.message : "The request failed. Try again.",
          }));
        }
        running = false;
        if (queued) {
          queued = false;
          refresh();
        }
      });
    };
    refreshRef.current = refresh;
    return () => {
      cancelled = true;
      refreshRef.current = null;
    };
  }, [context]);

  useEffect(() => {
    // Polls request a refresh without invalidating the current scan. Any
    // number of polls while busy becomes one trailing scan, so long reads
    // can finish and update the table without accumulating parallel work.
    refreshRef.current?.();
  }, [context, group, activeRollout]);

  // Do not display the preceding model's data under a new context, even in the
  // render before its request settles. Publish both lists together.
  const current = snapshot.context === context ? snapshot : undefined;
  const miners = current?.miners ?? null;
  const isLoading = current?.isLoading ?? true;
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
              if (!isLoading) refreshRef.current?.();
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
