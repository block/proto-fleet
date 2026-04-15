import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import Miners from "./Miners";
import { MinerDiscoveryMode } from "./types";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { DeviceSelectorSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import {
  Device,
  DiscoverRequest,
  DiscoverRequestSchema,
  PairRequestSchema,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useForemanImport } from "@/protoFleet/api/useForemanImport";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { defaultTimeout } from "@/protoFleet/features/onboarding/constants";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";
import { pushToast, removeToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { ManualDiscoveryTargets } from "@/shared/utils/networkDiscovery";

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
  /** Already-paired miner IDs to filter out from discovery results */
  pairedMinerIds?: string[];
  /** Callback to notify that pairing completed (triggers CompleteSetup refresh) */
  onPairingCompleted?: () => void;
  /** Callback to refetch the fleet miner list */
  onRefetchMiners?: () => void;
};

const MinersPage = ({
  mode = "onboarding",
  onExit,
  pairedMinerIds = [],
  onPairingCompleted,
  onRefetchMiners,
}: MinersPageProps) => {
  const navigate = useNavigate();

  const { data: networkInfo, pending: networkInfoPending } = useNetworkInfo();

  const { discover, pairingPending, pair } = useMinerPairing();
  const { importPending: foremanImportPending, importFromForeman, completeImport } = useForemanImport();
  const [scanDiscoveryPending, setScanDiscoveryPending] = useState(false);
  const [ipListDiscoveryPending, setIpListDiscoveryPending] = useState(false);
  const discoveryAbortController = useRef<AbortController>(new AbortController());
  const longPairingToastShown = useRef(false);
  const loadingToastIds = useRef<number[]>([]);
  const foremanCredsRef = useRef<{ apiKey: string; clientId: string } | null>(null);

  const [foundMiners, setFoundMiners] = useState<Device[]>([]);
  const [lastDiscoveryMode, setLastDiscoveryMode] = useState<string>(minerDiscoveryModes.scan);
  const [lastManualTargets, setLastManualTargets] = useState<ManualDiscoveryTargets | null>(null);

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
  // Process discovered miners, ensuring no duplicates
  const pairedMinerIdSet = useMemo(() => new Set(pairedMinerIds), [pairedMinerIds]);
  const processDiscoveredMiners = useCallback(
    (devices: Device[]) => {
      setFoundMiners((prevMiners) => {
        const newMiners = devices.filter(
          (device) =>
            !prevMiners.some((prevMiner) => prevMiner.deviceIdentifier === device.deviceIdentifier) &&
            !pairedMinerIdSet.has(device.deviceIdentifier),
        );
        return [...prevMiners, ...newMiners];
      });
    },
    [pairedMinerIdSet],
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
    if (!networkInfo?.subnet) return;

    foremanCredsRef.current = null;
    setFoundMiners([]);
    setScanDiscoveryPending(true);
    setLastDiscoveryMode(minerDiscoveryModes.scan);
    setLastManualTargets(null);
    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "nmap",
        value: {
          target: networkInfo.subnet,
        },
      },
    });
    const controller = discoveryAbortController.current;
    handleDiscover(discoverRequest, controller).finally(() => {
      if (!controller.signal.aborted) {
        setScanDiscoveryPending(false);
      }
    });
  }, [handleDiscover, networkInfo]);

  const cancelNetworkScan = useCallback(() => {
    foremanCredsRef.current = null;
    setScanDiscoveryPending(false);
    setIpListDiscoveryPending(false);
    setLastDiscoveryMode(minerDiscoveryModes.scan);
    setLastManualTargets(null);
    if (discoveryAbortController.current) {
      discoveryAbortController.current.abort();
      discoveryAbortController.current = new AbortController();
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

  const handleManualDiscovery = useCallback(
    async (targets: ManualDiscoveryTargets) => {
      setFoundMiners([]);
      setLastDiscoveryMode(minerDiscoveryModes.ipList);
      setLastManualTargets(targets);
      const discoverRequests: DiscoverRequest[] = [];

      if (targets.ipAddresses.length > 0) {
        discoverRequests.push(
          create(DiscoverRequestSchema, {
            mode: {
              case: "ipList",
              value: {
                ipAddresses: targets.ipAddresses,
              },
            },
          }),
        );
      }

      targets.subnets.forEach((subnet) => {
        discoverRequests.push(
          create(DiscoverRequestSchema, {
            mode: {
              case: "nmap",
              value: {
                target: subnet,
              },
            },
          }),
        );
      });

      targets.ipRanges.forEach((range) => {
        discoverRequests.push(
          create(DiscoverRequestSchema, {
            mode: {
              case: "ipRange",
              value: {
                startIp: range.startIp,
                endIp: range.endIp,
              },
            },
          }),
        );
      });

      if (discoverRequests.length === 0) return;

      const controller = discoveryAbortController.current;
      setIpListDiscoveryPending(true);
      try {
        await Promise.allSettled(discoverRequests.map((request) => handleDiscover(request, controller)));
      } finally {
        if (!controller.signal.aborted) {
          setIpListDiscoveryPending(false);
        }
      }
    },
    [handleDiscover],
  );

  const handleRescan = useCallback(() => {
    if (scanDiscoveryPending || ipListDiscoveryPending) return;

    const wasForeman = lastDiscoveryMode === minerDiscoveryModes.foreman;

    if ((lastDiscoveryMode === minerDiscoveryModes.ipList || wasForeman) && lastManualTargets) {
      handleManualDiscovery(lastManualTargets);
      // Preserve foreman mode so completeImport still fires after pairing
      if (wasForeman) {
        setLastDiscoveryMode(minerDiscoveryModes.foreman);
      }
    } else {
      handleNmapDiscovery();
    }
  }, [
    scanDiscoveryPending,
    ipListDiscoveryPending,
    lastDiscoveryMode,
    lastManualTargets,
    handleManualDiscovery,
    handleNmapDiscovery,
  ]);

  // Helper to clear all loading toasts
  function clearLoadingToasts() {
    loadingToastIds.current.forEach((id) => removeToast(id));
    loadingToastIds.current = [];
  }

  function handleContinue(selectedMinerIdentifiers: string[]) {
    // Abort any in-flight discovery before pairing
    discoveryAbortController.current.abort();
    discoveryAbortController.current = new AbortController();
    setScanDiscoveryPending(false);
    setIpListDiscoveryPending(false);

    // Clear any previous loading toasts and reset state
    clearLoadingToasts();

    // Show immediate feedback when user clicks Continue
    const toastId = pushToast({
      message: `Adding ${selectedMinerIdentifiers.length} miner${selectedMinerIdentifiers.length !== 1 ? "s" : ""} to fleet...`,
      status: TOAST_STATUSES.loading,
    });
    loadingToastIds.current.push(toastId);

    const pairRequest = create(PairRequestSchema, {
      deviceSelector: create(DeviceSelectorSchema, {
        selectionType: {
          case: "includeDevices",
          value: create(DeviceIdentifierListSchema, {
            deviceIdentifiers: selectedMinerIdentifiers,
          }),
        },
      }),
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

        // Notify that pairing completed so CompleteSetup can refetch pool status
        if (successCount > 0) {
          onPairingCompleted?.();
        }

        // If this was a Foreman import, create pools/groups/racks and assign devices before refreshing
        if (lastDiscoveryMode === minerDiscoveryModes.foreman && foremanCredsRef.current) {
          const creds = foremanCredsRef.current;
          foremanCredsRef.current = null;
          if (successCount > 0) {
            await completeImport({
              apiKey: creds.apiKey,
              clientId: creds.clientId,
              importPools: true,
              importGroups: true,
              importRacks: true,
              pairedDeviceIdentifiers: selectedMinerIdentifiers.filter((id) => !failedDeviceIds.includes(id)),
              onSuccess: () => {},
              onError: (error) => {
                console.error("Foreman complete import failed:", error);
                pushToast({
                  message: "Miners added but failed to import pools/groups/racks from Foreman.",
                  status: TOAST_STATUSES.error,
                });
              },
            });
          }
        }

        // Wait for fleet data to refresh with updated firmware versions before navigating
        await refetch();
        onRefetchMiners?.();
        // Notify that pairing completed so Dashboard and other components can refresh
        onPairingCompleted?.();
        if (mode === "onboarding") {
          navigate("/");
        } else {
          onExit?.();
        }
      },
      onError: (error) => {
        console.error("Pairing error:", error);
        foremanCredsRef.current = null;

        // Clear loading toasts before showing error
        clearLoadingToasts();
        pushToast({
          message: "Failed to add miners. Please check that miners are online and try again.",
          status: TOAST_STATUSES.error,
        });
      },
    });
  }

  const handleForemanImport = useCallback(
    (apiKey: string, clientId: string) => {
      foremanCredsRef.current = { apiKey, clientId };
      importFromForeman({
        apiKey,
        clientId,
        onSuccess: (response) => {
          const ips = response.miners.map((m) => m.ipAddress).filter((ip) => ip !== "");

          if (ips.length > 0) {
            handleManualDiscovery({ ipAddresses: ips, subnets: [], ipRanges: [] });
            // Override the ipList mode that handleManualDiscovery just set
            setLastDiscoveryMode(minerDiscoveryModes.foreman);
          } else {
            foremanCredsRef.current = null;
            pushToast({
              message: "No miners with IP addresses found in your Foreman account.",
              status: TOAST_STATUSES.error,
            });
          }
        },
        onError: (error) => {
          foremanCredsRef.current = null;
          pushToast({
            message: `Foreman import failed: ${error}`,
            status: TOAST_STATUSES.error,
          });
        },
      });
    },
    [importFromForeman, handleManualDiscovery],
  );

  return (
    <Miners
      foundMiners={foundMiners}
      scanDiscoveryPending={scanDiscoveryPending}
      ipListDiscoveryPending={ipListDiscoveryPending}
      pairingPending={pairingPending}
      networkInfoPending={networkInfoPending}
      scanAvailable={!!networkInfo?.subnet}
      onCancelScan={cancelNetworkScan}
      onManualDiscover={handleManualDiscovery}
      onContinue={handleContinue}
      onRescan={handleRescan}
      onForemanImport={handleForemanImport}
      foremanImportPending={foremanImportPending}
      mode={mode}
    />
  );
};

export default MinersPage;
