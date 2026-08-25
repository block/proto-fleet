import { useCallback, useEffect, useRef, useState } from "react";

import { fleetManagementClient, rolloutClient } from "@/protoFleet/api/clients";
import type { FirmwareAssignment, Rollout, RolloutLane } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const POLL_INTERVAL_MS = 5000;

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
  applyFirmware: (laneId: bigint, assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[]) => Promise<void>;
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
    async (laneId: bigint, assignments: Pick<FirmwareAssignment, "model" | "firmwareFileId">[]) => {
      await rolloutClient.applyRolloutLaneFirmware({
        laneId,
        assignments: assignments.map((a) => ({ model: a.model, firmwareFileId: a.firmwareFileId })),
      });
      await refresh();
    },
    [refresh],
  );

  return { lanes, rollouts, minerNames, isLoading, refresh, createLane, deleteLane, updateMembers, applyFirmware };
}
