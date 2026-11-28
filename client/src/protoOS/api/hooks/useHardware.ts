import { useCallback, useEffect, useMemo, useState } from "react";

import { TOTAL_FAN_SLOTS, TOTAL_HASHBOARD_SLOTS, TOTAL_PSU_SLOTS } from "../constants";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";

const fillSlots = <T extends { slot?: number }>(items: T[], count: number) => {
  const bySlot = new Map(items.map((item) => [item.slot, item]));
  return Array.from({ length: count }, (_, index) => bySlot.get(index + 1) ?? null);
};

const useHardware = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const hardware = useMinerStore((state) => state.hardware);
  const systemInfo = useMinerStore((state) => state.systemInfo.data);

  const fetchHardware = useCallback(() => {
    if (!adapter) return;

    setPending(true);
    const accept = beginStoreWrite("hardware");
    setError(undefined);
    return authRetry({
      request: (params) =>
        adapter.getHardware(
          {
            deviceId: String(systemInfo?.identity.deviceId ?? "device:unknown"),
            profile: systemInfo?.moduleModel ?? "unknown",
          },
          params,
        ),
      onSuccess: ({ hardware: normalized, telemetry }) => {
        if (!accept()) return;
        useMinerStore.getState().hardware.replaceHardware(normalized, accept.requestId);
        useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
      },
      onError: (err) => setError(err?.error?.message ?? "An error occurred"),
    }).finally(() => setPending(false));
  }, [adapter, authRetry, systemInfo?.identity.deviceId, systemInfo?.moduleModel]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- hydrate from the external miner API when the active adapter changes
    fetchHardware();
  }, [fetchHardware]);

  const view = useMemo(() => {
    const controlBoardInfo = [...hardware.controlBoards.values()][0];
    const hashboards = [...hardware.hashboards.values()].map((board) => {
      const node = hardware.topology?.hashboards.get(board.ref.id);
      return { ...board, slot: node?.location?.slot, bay: node?.location?.bay, port: node?.location?.port };
    });
    return {
      data: hardware,
      controlBoardInfo,
      hashboardsInfo: fillSlots(hashboards, TOTAL_HASHBOARD_SLOTS),
      psusInfo: fillSlots([...hardware.psus.values()], TOTAL_PSU_SLOTS),
      fansInfo: fillSlots([...hardware.fans.values()], TOTAL_FAN_SLOTS),
    };
  }, [hardware]);

  return useMemo(() => ({ pending, error, ...view }), [pending, error, view]);
};

export { useHardware };
