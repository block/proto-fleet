import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Granular Hooks
// =============================================================================

/**
 * Hook to get all network info data
 * Returns the entire networkInfo slice
 * Note: This will re-render on any field change. For better performance, use specific field hooks.
 */
export const useNetworkInfo = () => useMinerStore(useShallow((state) => state.networkInfo));

/**
 * Hook to get specific network info fields
 */
export const useHostname = () => useMinerStore((state) => state.networkInfo.hostname);

export const useIpAddress = () => useMinerStore((state) => state.networkInfo.ip);

export const useMacAddress = () => useMinerStore((state) => state.networkInfo.mac);

export const useGateway = () => useMinerStore((state) => state.networkInfo.gateway);

export const useNetmask = () => useMinerStore((state) => state.networkInfo.netmask);

export const useDhcp = () => useMinerStore((state) => state.networkInfo.dhcp);

export const useNetworkInfoPending = () => useMinerStore((state) => state.networkInfo.pending ?? false);

export const useNetworkInfoError = () => useMinerStore((state) => state.networkInfo.error);

// =============================================================================
// Action Hooks
// =============================================================================

export const useSetNetworkInfo = () => useMinerStore((state) => state.networkInfo.setNetworkInfo);

export const useSetNetworkInfoError = () => useMinerStore((state) => state.networkInfo.setError);

export const useSetNetworkInfoPending = () => useMinerStore((state) => state.networkInfo.setPending);
