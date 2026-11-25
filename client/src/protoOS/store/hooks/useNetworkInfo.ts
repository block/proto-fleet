import useMinerStore from "../useMinerStore";

const primaryInterface = (state: ReturnType<typeof useMinerStore.getState>) =>
  [...state.networkInfo.interfaces.values()].find((network) => network.isPrimary) ??
  state.networkInfo.interfaces.values().next().value;

export const useNetworkInfo = () => useMinerStore(primaryInterface);
export const useHostname = () => useMinerStore((state) => primaryInterface(state)?.hostname);
export const useIpAddress = () => useMinerStore((state) => primaryInterface(state)?.ipAddress);
export const useMacAddress = () => useMinerStore((state) => primaryInterface(state)?.macAddress);
export const useGateway = () => useMinerStore((state) => primaryInterface(state)?.gateway);
export const useNetmask = () => useMinerStore((state) => primaryInterface(state)?.netmask);
export const useDhcp = () => useMinerStore((state) => primaryInterface(state)?.dhcp);
export const useNetworkInfoPending = () => useMinerStore((state) => state.networkInfo.resource.status === "loading");
export const useNetworkInfoError = () =>
  useMinerStore((state) =>
    state.networkInfo.resource.status === "error" ? state.networkInfo.resource.error.message : undefined,
  );
export const useSetNetworkInfo = () => useMinerStore((state) => state.networkInfo.setNetworkInfo);
export const useSetNetworkInfoError = () => useMinerStore((state) => state.networkInfo.setError);
export const useSetNetworkInfoPending = () => useMinerStore((state) => state.networkInfo.setPending);
