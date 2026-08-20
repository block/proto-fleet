import type { DeviceType } from "../contract";
import useMinerStore from "../useMinerStore";

const resourceError = (resource: ReturnType<typeof useMinerStore.getState>["systemInfo"]["resource"]) =>
  resource.status === "error" ? resource.error.message : undefined;

export const useSystemInfo = () => useMinerStore((state) => state.systemInfo.data);
export const useProductName = () => useMinerStore((state) => state.systemInfo.data?.identity.productName);
export const useSerialNumber = () => useMinerStore((state) => state.systemInfo.data?.identity.serialNumber);
export const useOSVersion = () => useMinerStore((state) => state.systemInfo.data?.operatingSystem?.version);
export const useFwUpdateStatus = () => useMinerStore((state) => state.systemInfo.data?.firmwareUpdate);
export const useSystemInfoPending = () => useMinerStore((state) => state.systemInfo.resource.status === "loading");
export const useSystemInfoError = () => useMinerStore((state) => resourceError(state.systemInfo.resource));
export const useIsProtoRig = () =>
  useMinerStore((state) => state.systemInfo.data?.identity.productName === "Proto Rig");
export const useDeviceType = (): DeviceType =>
  useMinerStore((state) => state.systemInfo.data?.identity.deviceType ?? "rig");
export const useModuleModel = () => useMinerStore((state) => state.systemInfo.data?.moduleModel ?? "unknown");
export const useIsWebServerRunning = () =>
  useMinerStore((state) => state.systemInfo.data?.webServer?.health === "running");
export const useIsMiningDriverRunning = () =>
  useMinerStore((state) => state.systemInfo.data?.miningDriver?.health === "running");
export const useHasFirmwareUpdate = () =>
  useMinerStore((state) => state.systemInfo.data?.firmwareUpdate?.status === "available");
export const useFirmwareUpdateInstalling = () =>
  useMinerStore((state) => {
    const status = state.systemInfo.data?.firmwareUpdate?.status;
    return status === "downloading" || status === "downloaded" || status === "installing" || status === "confirming";
  });

export const useSetSystemInfo = () => useMinerStore((state) => state.systemInfo.setSystemInfo);
export const useSetSystemInfoError = () => useMinerStore((state) => state.systemInfo.setError);
export const useSetSystemInfoPending = () => useMinerStore((state) => state.systemInfo.setPending);
