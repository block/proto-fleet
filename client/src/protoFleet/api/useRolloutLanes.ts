import { useCallback, useEffect, useRef, useState } from "react";

import { fleetManagementClient, rolloutClient } from "@/protoFleet/api/clients";
import {
  type FirmwareAssignment,
  type Rollout,
  type RolloutLane,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const POLL_INTERVAL_MS = 5000;

// How rollouts started by an apply call run; omitted means all at once.
export interface ApplyRolloutOptions {
  method: RolloutMethod;
  // Miners per batch (the pilot size for PILOT); ignored for IMMEDIATE.
  batchSize: number;
  // Release each review gate automatically once the thresholds hold.
  autoAdvance: boolean;
  // Largest acceptable aggregate hashrate drop, in percent; 0 disables.
  maxHashrateDropPercent: number;
  stabilizationSeconds: number;
}

export const IMMEDIATE_ROLLOUT_OPTIONS: ApplyRolloutOptions = {
  method: RolloutMethod.IMMEDIATE,
  batchSize: 0,
  autoAdvance: false,
  maxHashrateDropPercent: 0,
  stabilizationSeconds: 0,
};

export interface AbortRolloutResult {
  // True when the previous assignment was restored, false when cleared.
  restoredPrevious: boolean;
  previousFirmwareVersion: string;
}

export interface RolloutLanesApi {
  lanes: RolloutLane[];
  rollouts: Rollout[];
  // deviceIdentifier -> display name, from fleet snapshots.
  minerNames: Record<string, string>;
  isLoading: boolean;
  refresh: () => Promise<void>;
  createLane: (name: string) => Promise<void>;
  deleteLane: (laneId: bigint) => Promise<void>;
  updateMembers: (laneId: bigint, add: string[], remove: string[]) => Promise<void>;
  applyFirmware: (
    laneId: bigint,
    assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[],
    options?: ApplyRolloutOptions,
  ) => Promise<void>;
  rollbackFirmware: (rolloutId: bigint) => Promise<void>;
  continueRollout: (rolloutId: bigint) => Promise<void>;
  pauseRollout: (rolloutId: bigint) => Promise<void>;
  resumeRollout: (rolloutId: bigint) => Promise<void>;
  abortRollout: (rolloutId: bigint) => Promise<AbortRolloutResult>;
}

// Fetches rollout lanes and rollouts, polling while mounted so firmware
// versions and rollout progress stay live.
export function useRolloutLanes(): RolloutLanesApi {
  const [lanes, setLanes] = useState<RolloutLane[]>([]);
  const [rollouts, setRollouts] = useState<Rollout[]>([]);
  const [minerNames, setMinerNames] = useState<Record<string, string>>({});
  const [isLoading, setIsLoading] = useState(true);
  const inFlightRef = useRef(false);

  const refresh = useCallback(async () => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    try {
      const [lanesResp, rolloutsResp, minersResp] = await Promise.all([
        rolloutClient.listRolloutLanes({}),
        rolloutClient.listRollouts({}),
        fleetManagementClient.listMinerStateSnapshots({ pageSize: 500 }),
      ]);
      setLanes(lanesResp.lanes);
      setRollouts(rolloutsResp.rollouts);
      setMinerNames(Object.fromEntries(minersResp.miners.map((miner) => [miner.deviceIdentifier, miner.name])));
    } finally {
      inFlightRef.current = false;
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh().catch((error) => console.error("Failed to load rollout lanes", error));
    const timer = setInterval(() => {
      refresh().catch((error) => console.error("Failed to refresh rollout lanes", error));
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  const createLane = useCallback(
    async (name: string) => {
      await rolloutClient.createRolloutLane({ name });
      await refresh();
    },
    [refresh],
  );

  const deleteLane = useCallback(
    async (laneId: bigint) => {
      await rolloutClient.deleteRolloutLane({ laneId });
      await refresh();
    },
    [refresh],
  );

  const updateMembers = useCallback(
    async (laneId: bigint, add: string[], remove: string[]) => {
      await rolloutClient.updateRolloutLaneMembers({
        laneId,
        addDeviceIdentifiers: add,
        removeDeviceIdentifiers: remove,
      });
      await refresh();
    },
    [refresh],
  );

  const applyFirmware = useCallback(
    async (
      laneId: bigint,
      assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[],
      options?: ApplyRolloutOptions,
    ) => {
      const { method, batchSize, autoAdvance, maxHashrateDropPercent, stabilizationSeconds } =
        options ?? IMMEDIATE_ROLLOUT_OPTIONS;
      await rolloutClient.applyRolloutLaneFirmware({
        laneId,
        assignments: assignments.map((a) => ({ model: a.model, firmwareFileId: a.firmwareFileId })),
        options: { method, batchSize, autoAdvance, maxHashrateDropPercent, stabilizationSeconds },
      });
      await refresh();
    },
    [refresh],
  );

  const rollbackFirmware = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.rollbackRolloutLaneFirmware({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const continueRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.continueRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const pauseRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.pauseRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const resumeRollout = useCallback(
    async (rolloutId: bigint) => {
      await rolloutClient.resumeRollout({ rolloutId });
      await refresh();
    },
    [refresh],
  );

  const abortRollout = useCallback(
    async (rolloutId: bigint): Promise<AbortRolloutResult> => {
      const resp = await rolloutClient.abortRollout({ rolloutId });
      await refresh();
      return {
        restoredPrevious: resp.restoredPrevious,
        previousFirmwareVersion: resp.rollout?.previousFirmwareVersion ?? "",
      };
    },
    [refresh],
  );

  return {
    lanes,
    rollouts,
    minerNames,
    isLoading,
    refresh,
    createLane,
    deleteLane,
    updateMembers,
    applyFirmware,
    rollbackFirmware,
    continueRollout,
    pauseRollout,
    resumeRollout,
    abortRollout,
  };
}
