import { type ReactNode, useCallback } from "react";
import { FeedMappingContext } from "./feedMappingContext";
import { useFeedMapping } from "./useFeedMapping";
import type { FeedMapping } from "@/protoOS/api/adapters/feedMapping";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

/** One mapping lifecycle for the dialog and callouts, including after navigation. */
export function FeedMappingProvider({
  mapping,
  deviceId,
  onSettled,
  children,
}: {
  mapping: FeedMapping;
  deviceId: string;
  onSettled?: (signal: AbortSignal) => Promise<void>;
  children: ReactNode;
}) {
  const state = useFeedMapping(mapping, deviceId, onSettled);
  return <FeedMappingContext.Provider value={state}>{children}</FeedMappingContext.Provider>;
}

export default function ContainerFeedMappingProvider({ children }: { children: ReactNode }) {
  const { adapter, isFleetHosted } = useMinerHosting();
  const generation = useMinerStore((state) => state.connectionGeneration);
  const identity = useMinerStore((state) => state.systemInfo.data?.identity);
  const deviceId = identity?.deviceId;
  const refreshHardware = useCallback(
    async (signal: AbortSignal) => {
      if (!adapter || !deviceId) return;
      const accept = beginStoreWrite("hardware");
      const result = await adapter.getHardware({ deviceId: String(deviceId), profile: "ct1" }, { signal });
      if (signal.aborted || !accept()) return;
      useMinerStore.getState().hardware.replaceHardware(result.hardware, accept.requestId);
      useMinerStore.getState().telemetry.mergeTelemetry(result.telemetry, accept.requestId);
    },
    [adapter, deviceId],
  );
  if (isFleetHosted || identity?.deviceType !== "container" || !adapter?.feedMapping || !deviceId) return children;
  return (
    <FeedMappingProvider
      key={`${deviceId}:${generation}`}
      mapping={adapter.feedMapping}
      deviceId={String(deviceId)}
      onSettled={refreshHardware}
    >
      {children}
    </FeedMappingProvider>
  );
}
