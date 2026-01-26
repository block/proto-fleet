import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import Miners from "./Miners";
import { MinerDiscoveryMode } from "./types";
import {
  Device,
  DiscoverRequest,
  DiscoverRequestSchema,
  PairRequestSchema,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { defaultDiscoveryPorts, defaultTimeout } from "@/protoFleet/features/onboarding/constants";
import { useFleetStore, useMinerIds, useNotifyPairingCompleted } from "@/protoFleet/store";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

// Show a toast if pairing takes longer than this threshold
const LONG_PAIRING_THRESHOLD_MS = 3000;

type MinersPageProps = {
  /**
   * Discovery mode determines the flow context and post-pairing behavior:
   * - 'onboarding': Used during initial setup flow, navigates to home after pairing
   * - 'pairing': Used for adding miners to existing fleet, calls onExit and reloads after pairing
   * @default 'onboarding'
   */
  mode?: MinerDiscoveryMode;
  /**
   * Callback invoked when user exits the discovery flow (e.g., cancels scan)
   * Only used when mode is 'pairing'
   */
  onExit?: () => void;
};

const MinersPage = ({ mode = "onboarding", onExit }: MinersPageProps) => {
  const navigate = useNavigate();

  const { data: networkInfo } = useNetworkInfo();

  const { discover, pairingPending, pair } = useMinerPairing();
  const [scanDiscoveryPending, setScanDiscoveryPending] = useState(false);
  const [ipListDiscoveryPending, setIpListDiscoveryPending] = useState(false);
  const scanAbortController = useRef<AbortController>(new AbortController());
  const longPairingToastShown = useRef(false);
  const loadingToastIds = useRef<number[]>([]);

  const [foundMiners, setFoundMiners] = useState<Device[]>([]);

  // Show a toast if pairing takes longer than the threshold
  useEffect(() => {
    if (!pairingPending) {
      longPairingToastShown.current = false;
      return;
    }

    const timeoutId = setTimeout(() => {
      if (pairingPending && !longPairingToastShown.current) {
        longPairingToastShown.current = true;
        const toastId = pushToast({
          message: "Adding miners is taking longer than expected. Some miners may be slow to respond.",
          status: TOAST_STATUSES.loading,
        });
        loadingToastIds.current.push(toastId);
      }
    }, LONG_PAIRING_THRESHOLD_MS);

    return () => clearTimeout(timeoutId);
  }, [pairingPending]);

  // Clean up loading toasts on unmount to prevent lingering toasts if user navigates away
  useEffect(() => {
    return () => {
      loadingToastIds.current.forEach((id) => removeToast(id));
    };
  }, []);

  const { refetch } = useOnboardedStatus();
  const notifyPairingCompleted = useNotifyPairingCompleted();

  // Get refetch callback from global store instead of creating a new useFleet instance
  // This avoids overwriting the Fleet component's refetch callback
  const refetchFleet = () => {
    const callback = useFleetStore.getState().fleet.refetchMiners;
    callback?.();
  };

  const minerIds = useMinerIds();
  // Process discovered miners, ensuring no duplicates
  const processDiscoveredMiners = useCallback(
    (devices: Device[]) => {
      setFoundMiners((prevMiners) => {
        const newMiners = devices.filter(
          (device) =>
            !prevMiners.some((prevMiner) => prevMiner.deviceIdentifier === device.deviceIdentifier) &&
            !minerIds.some((minerId) => minerId === device.deviceIdentifier),
        );
        return [...prevMiners, ...newMiners];
      });
    },
    [minerIds],
  );

  const handleDiscover = useCallback(
    (discoverRequest: DiscoverRequest, abortController?: AbortController) => {
      return discover({
        discoverRequest: discoverRequest,
        discoverAbortController: abortController,
        onStreamData: processDiscoveredMiners,
        onError: (error) => {
          console.error("Discovery error:", error);
          pushToast({
            message: "Discovery failed",
            status: TOAST_STATUSES.error,
          });
        },
      });
    },
    [discover, processDiscoveredMiners],
  );

  const handleNmapDiscovery = useCallback(() => {
    if (!networkInfo) return;

    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "nmap",
        value: {
          target: networkInfo.subnet,
          ports: defaultDiscoveryPorts,
        },
      },
    });
    setScanDiscoveryPending(true);
    handleDiscover(discoverRequest, scanAbortController.current).finally(() => setScanDiscoveryPending(false));
  }, [handleDiscover, networkInfo]);

  const cancelNetworkScan = useCallback(() => {
    if (scanAbortController.current) {
      scanAbortController.current.abort();
      scanAbortController.current = new AbortController();
    }
    onExit?.();
  }, [onExit]);

  const handleMdnsDiscovery = useCallback(() => {
    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "mdns",
        value: {
          serviceType: "_fleet._tcp",
          domain: "local",
          timeoutSeconds: defaultTimeout,
        },
      },
    });
    handleDiscover(discoverRequest);
  }, [handleDiscover]);
  void handleMdnsDiscovery;

  const handleIpListDiscovery = useCallback(
    (ipAddresses: string[]) => {
      const discoverRequest = create(DiscoverRequestSchema, {
        mode: {
          case: "ipList",
          value: {
            ipAddresses: ipAddresses,
            ports: defaultDiscoveryPorts,
          },
        },
      });
      setIpListDiscoveryPending(true);
      handleDiscover(discoverRequest).finally(() => setIpListDiscoveryPending(false));
    },
    [handleDiscover],
  );

  const handleRescan = useCallback(() => {
    // do not rescan if scan is already in progress
    if (!scanDiscoveryPending) {
      handleNmapDiscovery();
    }
  }, [scanDiscoveryPending, handleNmapDiscovery]);

  // Helper to clear all loading toasts
  function clearLoadingToasts() {
    loadingToastIds.current.forEach((id) => removeToast(id));
    loadingToastIds.current = [];
  }

  function handleContinue(selectedMinerIdentifiers: string[]) {
    // Clear any previous loading toasts and reset state
    clearLoadingToasts();

    // Show immediate feedback when user clicks Continue
    const toastId = pushToast({
      message: `Adding ${selectedMinerIdentifiers.length} miner${selectedMinerIdentifiers.length !== 1 ? "s" : ""} to fleet...`,
      status: TOAST_STATUSES.loading,
    });
    loadingToastIds.current.push(toastId);

    const pairRequest = create(PairRequestSchema, {
      deviceIdentifiers: selectedMinerIdentifiers,
    });
    pair({
      pairRequest: pairRequest,
      onSuccess: async (failedDeviceIds) => {
        // Clear loading toasts before showing result
        clearLoadingToasts();

        const successCount = selectedMinerIdentifiers.length - failedDeviceIds.length;
        const failedCount = failedDeviceIds.length;

        // Show appropriate toast based on results
        if (failedCount > 0 && successCount > 0) {
          // Partial success - some miners failed
          pushToast({
            message: `Added ${successCount} miner${successCount !== 1 ? "s" : ""}. ${failedCount} miner${failedCount !== 1 ? "s" : ""} could not be reached.`,
            status: TOAST_STATUSES.error,
          });
        } else if (failedCount > 0 && successCount === 0) {
          // All failed
          pushToast({
            message: `Failed to add ${failedCount} miner${failedCount !== 1 ? "s" : ""}. Please check that miners are online and try again.`,
            status: TOAST_STATUSES.error,
          });
        } else if (successCount > 0) {
          // All succeeded
          pushToast({
            message: `Successfully added ${successCount} miner${successCount !== 1 ? "s" : ""} to fleet.`,
            status: TOAST_STATUSES.success,
          });
        }

        // Wait for fleet data to refresh with updated firmware versions before navigating
        await refetch();
        refetchFleet();
        // Notify store that pairing completed so Dashboard and other components can refresh
        notifyPairingCompleted();
        if (mode === "onboarding") {
          navigate("/");
        } else {
          onExit?.();
        }
      },
      onError: (error) => {
        console.error("Pairing error:", error);

        // Clear loading toasts before showing error
        clearLoadingToasts();
        pushToast({
          message: "Failed to add miners. Please check that miners are online and try again.",
          status: TOAST_STATUSES.error,
        });
      },
    });
  }

  return (
    <Miners
      foundMiners={foundMiners}
      scanDiscoveryPending={scanDiscoveryPending}
      ipListDiscoveryPending={ipListDiscoveryPending}
      pairingPending={pairingPending}
      onCancelScan={cancelNetworkScan}
      onIpListModeDiscover={handleIpListDiscovery}
      onContinue={handleContinue}
      onRescan={handleRescan}
      onClearFoundMiners={() => setFoundMiners([])}
      mode={mode}
    />
  );
};

export default MinersPage;
