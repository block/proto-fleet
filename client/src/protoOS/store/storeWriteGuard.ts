import { asResourceKey } from "./contract";
import useMinerStore from "./useMinerStore";

/**
 * Accept callback returned by {@link beginStoreWrite}. Calling it returns whether
 * this write is still the newest for its resource key. `requestId` exposes the
 * monotonic token minted at begin time so per-entity write paths (e.g. telemetry)
 * can enforce newest-write-wins across differently-keyed callers.
 */
export interface StoreWriteAccept {
  (): boolean;
  requestId: number;
}

export const beginStoreWrite = (resourceName: string): StoreWriteAccept => {
  const store = useMinerStore.getState();
  const resource = asResourceKey(resourceName);
  const token = store.beginStoreWrite();
  const accept = (() => useMinerStore.getState().acceptStoreWrite(resource, token)) as StoreWriteAccept;
  accept.requestId = token.requestId;
  return accept;
};
