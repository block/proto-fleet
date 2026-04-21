import { useShallow } from "zustand/react/shallow";
import useMinerStore from "../useMinerStore";

const PROTO_RIG_PRODUCT_NAME = "Proto Rig";

// =============================================================================
// Granular Hooks
// =============================================================================

/**
 * Hook to get all system info data
 * Returns the entire systemInfo slice
 * Note: This will re-render on any field change. For better performance, use specific field hooks.
 */
export const useSystemInfo = () => useMinerStore(useShallow((state) => state.systemInfo));

/**
 * Hook to get specific system info fields
 */
export const useProductName = () => useMinerStore((state) => state.systemInfo.product_name);

export const useSerialNumber = () => useMinerStore((state) => state.systemInfo.cb_sn);

export const useOSVersion = () => useMinerStore((state) => state.systemInfo.os?.version);

export const useFwUpdateStatus = () => useMinerStore((state) => state.systemInfo.sw_update_status);

/**
 * Hook to get the system info pending state
 */
export const useSystemInfoPending = () => useMinerStore((state) => state.systemInfo.pending ?? false);

/**
 * Hook to get the system info error
 */
export const useSystemInfoError = () => useMinerStore((state) => state.systemInfo.error);

/**
 * Hook to check if the device is a Proto Rig
 */
export const useIsProtoRig = () => {
  return useMinerStore((state) => {
    return state.systemInfo.product_name === PROTO_RIG_PRODUCT_NAME;
  });
};

/**
 * Hook to check if the web server is running
 * Returns true if we have system info data (meaning web server responded)
 */
export const useIsWebServerRunning = () => {
  return useMinerStore((state) => {
    // If we have product_name or any other field, web server is running
    const isRunning = !!state.systemInfo.product_name;
    return isRunning;
  });
};

/**
 * Hook to check if the mining driver is running
 */
export const useIsMiningDriverRunning = () => {
  return useMinerStore((state) => {
    const miningDriverSwName = state.systemInfo.mining_driver_sw?.name;
    const isRunning =
      miningDriverSwName !== undefined &&
      !/tcp connect error: Connection refused|Failed to connect to MinerDataApiClient/.test(miningDriverSwName);
    return isRunning;
  });
};

/**
 * Hook to check if a firmware update is available
 */
export const useHasFirmwareUpdate = () => {
  return useMinerStore((state) => {
    return state.systemInfo.sw_update_status?.status === "available";
  });
};

/**
 * Hook to check if firmware is currently installing
 * (includes downloading, downloaded, installing, confirming states)
 */
export const useFirmwareUpdateInstalling = () => {
  return useMinerStore((state) => {
    const status = state.systemInfo.sw_update_status?.status;
    return status === "downloading" || status === "downloaded" || status === "installing" || status === "confirming";
  });
};

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Hook to get the setSystemInfo action
 */
export const useSetSystemInfo = () => useMinerStore((state) => state.systemInfo.setSystemInfo);

/**
 * Hook to get the setError action
 */
export const useSetSystemInfoError = () => useMinerStore((state) => state.systemInfo.setError);

/**
 * Hook to get the setPending action
 */
export const useSetSystemInfoPending = () => useMinerStore((state) => state.systemInfo.setPending);
