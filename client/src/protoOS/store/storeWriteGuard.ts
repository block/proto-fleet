import { asResourceKey } from "./contract";
import useMinerStore from "./useMinerStore";

export const beginStoreWrite = (resourceName: string) => {
  const store = useMinerStore.getState();
  if (!store.beginStoreWrite || !store.acceptStoreWrite) return () => true;
  const resource = asResourceKey(resourceName);
  const token = store.beginStoreWrite();
  return () => useMinerStore.getState().acceptStoreWrite(resource, token);
};
