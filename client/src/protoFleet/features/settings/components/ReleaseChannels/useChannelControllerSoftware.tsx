import { type ReactNode, useCallback, useMemo, useRef, useState } from "react";

import ControllerSoftwareContainersModal from "./ControllerSoftwareContainersModal";
import { type ControllerSoftwareApi, controllerSoftwareVersionKey } from "./controllerSoftwareUtils";
import { pairKey, pairLabel } from "./rolloutStatus";
import { useRefreshingRead } from "./useRefreshingRead";
import type { ReleaseChannelControllerSoftware } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { trimMinerTarget } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface ControllerSoftwarePair {
  manufacturer: string;
  model: string;
}

interface PendingChange extends ControllerSoftwarePair {
  label: string;
  version: string;
  current: string;
}

const samePair = (a: ControllerSoftwarePair, b: ControllerSoftwarePair): boolean =>
  a.manufacturer === b.manufacturer && a.model === b.model;

export interface ChannelControllerSoftware {
  canSet: boolean;
  // Null until the first read completes.
  models: ReleaseChannelControllerSoftware[] | null;
  versions: readonly string[];
  error: string | null;
  versionsError: string | null;
  isSetting: boolean;
  retry: () => void;
  forPair: (pair: ControllerSoftwarePair) => ReleaseChannelControllerSoftware | undefined;
  targetForPair: (pair: ControllerSoftwarePair) => string;
  isRefreshingPair: (pair: ControllerSoftwarePair) => boolean;
  requestChange: (pair: ControllerSoftwarePair, version: string) => void;
  viewContainers: (pair: ControllerSoftwarePair) => void;
  // The confirmation and the container list, rendered outside the table.
  dialogs: ReactNode;
}

// Reads a channel's controller software versions while it has containers, and
// owns the confirmation that changes one. The version is saved on its own, not
// with the channel's pending firmware changes: it starts no update.
export function useChannelControllerSoftware({
  api,
  channelId,
  channelName,
  enabled,
  refreshKey,
  minerNames,
  writeLock,
}: {
  api: ControllerSoftwareApi | undefined;
  channelId: bigint | undefined;
  channelName: string;
  // False for a channel without containers, which then makes no request.
  enabled: boolean;
  // Changes with each channel poll, so counts track containers as they update.
  refreshKey: unknown;
  minerNames: Record<string, string>;
  writeLock?: { isLocked: boolean; tryAcquire: () => boolean; release: () => void };
}): ChannelControllerSoftware {
  const active = enabled && api !== undefined && channelId !== undefined;
  const list = api?.list;
  const listVersions = api?.listVersions;
  const canSet = api?.canSet ?? false;
  // A response started before a successful write cannot acknowledge that write.
  const writeRevision = useRef(0);
  const [savedVersions, setSavedVersions] = useState<Record<string, { version: string; revision: number }>>({});
  const read = useCallback(
    async (signal: AbortSignal) => {
      const revision = writeRevision.current;
      const models = list && channelId !== undefined ? await list(channelId, signal) : [];
      return { models, revision };
    },
    [list, channelId],
  );
  const software = useRefreshingRead({
    read,
    enabled: active,
    refreshKey,
    trailingRefresh: true,
    errorMessage: "Couldn't load controller software versions.",
  });
  const readVersions = useCallback(
    (signal: AbortSignal) => (listVersions ? listVersions(signal) : Promise.resolve([])),
    [listVersions],
  );
  const uploaded = useRefreshingRead({
    read: readVersions,
    enabled: active && canSet,
    errorMessage: "Couldn't load uploaded controller software.",
  });

  const [pending, setPending] = useState<PendingChange | null>(null);
  const [isSetting, setIsSetting] = useState(false);
  const [setError, setSetError] = useState<string | null>(null);
  const [containersPair, setContainersPair] = useState<ControllerSoftwarePair | null>(null);
  const setInFlightRef = useRef(false);

  const models = software.data?.models ?? null;
  const versions = useMemo(() => uploaded.data ?? [], [uploaded.data]);
  const forPair = useCallback(
    (pair: ControllerSoftwarePair) => models?.find((model) => samePair(model, pair)),
    [models],
  );
  const unconfirmedSave = useCallback(
    (pair: ControllerSoftwarePair) => {
      const saved = savedVersions[pairKey(pair)];
      return saved && (software.data?.revision ?? -1) < saved.revision ? saved : undefined;
    },
    [savedVersions, software.data],
  );
  const targetForPair = useCallback(
    (pair: ControllerSoftwarePair) => unconfirmedSave(pair)?.version ?? forPair(pair)?.targetVersion ?? "",
    [unconfirmedSave, forPair],
  );
  const isRefreshingPair = useCallback(
    (pair: ControllerSoftwarePair) => unconfirmedSave(pair) !== undefined,
    [unconfirmedSave],
  );
  const refreshSoftware = software.refresh;

  const requestChange = useCallback(
    (pair: ControllerSoftwarePair, version: string) => {
      const current = targetForPair(pair);
      if (!canSet || controllerSoftwareVersionKey(version) === controllerSoftwareVersionKey(current)) return;
      setSetError(null);
      setPending({ ...pair, label: pairLabel(pair), version, current });
    },
    [canSet, targetForPair],
  );

  const confirm = async () => {
    if (!api || channelId === undefined || !pending || setInFlightRef.current) return;
    if (writeLock && !writeLock.tryAcquire()) return;
    setInFlightRef.current = true;
    setIsSetting(true);
    setSetError(null);
    try {
      await api.set(channelId, trimMinerTarget(pending.manufacturer), trimMinerTarget(pending.model), pending.version);
      const revision = ++writeRevision.current;
      setSavedVersions((previous) => ({ ...previous, [pairKey(pending)]: { version: pending.version, revision } }));
      pushToast({
        message:
          pending.version === ""
            ? "Controller software version cleared"
            : `Controller software version set to ${pending.version}`,
        status: STATUSES.success,
      });
      setPending(null);
      refreshSoftware();
    } catch (error) {
      setSetError(
        error instanceof Error && error.message ? error.message : "Couldn't set the controller software version.",
      );
    } finally {
      setInFlightRef.current = false;
      setIsSetting(false);
      writeLock?.release();
    }
  };

  const containersModel = containersPair ? forPair(containersPair) : undefined;

  const dialogs = (
    <>
      {pending ? (
        <Dialog
          open
          title={
            pending.version === ""
              ? "Stop requiring a controller software version?"
              : `Require controller software ${pending.version}?`
          }
          subtitle={
            pending.version === ""
              ? `Firmware updates for ${pending.label} in ${channelName} will no longer wait for controller software ${pending.current}.`
              : `${pending.label} containers in ${channelName} that aren't on ${pending.version} won't receive firmware updates until their controller software is updated. This doesn't update any container.`
          }
          testId="controller-software-dialog"
          onDismiss={() => {
            if (!isSetting) setPending(null);
          }}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setPending(null), disabled: isSetting },
            {
              text: pending.version === "" ? "Clear version" : "Set version",
              variant: variants.primary,
              onClick: confirm,
              loading: isSetting,
            },
          ]}
        >
          {setError ? (
            <p role="alert" className="text-200 text-intent-critical-fill">
              {setError}
            </p>
          ) : null}
        </Dialog>
      ) : null}
      {containersPair && containersModel && api && channelId !== undefined ? (
        <ControllerSoftwareContainersModal
          channelId={channelId}
          channelName={channelName}
          model={containersModel}
          targetVersion={targetForPair(containersPair)}
          listContainers={api.listContainers}
          minerNames={minerNames}
          onClose={() => setContainersPair(null)}
        />
      ) : null}
    </>
  );

  return {
    canSet,
    models,
    versions,
    error: software.error,
    versionsError: uploaded.error,
    isSetting,
    retry: refreshSoftware,
    forPair,
    targetForPair,
    isRefreshingPair,
    requestChange,
    viewContainers: setContainersPair,
    dialogs,
  };
}
