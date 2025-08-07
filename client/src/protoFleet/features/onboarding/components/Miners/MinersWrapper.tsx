import { useCallback, useRef, useState } from "react";
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
import { useMinerIds } from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import {
  defaultDiscoveryPorts,
  defaultTimeout,
} from "@/protoFleet/features/onboarding/constants";
import { useOnboardingContext } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

type MinersPageProps = {
  mode?: MinerDiscoveryMode;
  onExit?: () => void;
};

const MinersPage = ({ mode = "onboarding", onExit }: MinersPageProps) => {
  const navigate = useNavigate();

  const { data: networkInfo } = useNetworkInfo();

  const { discover, pairingPending, pair } = useMinerPairing();
  const [scanDiscoveryPending, setScanDiscoveryPending] = useState(false);
  const [ipListDiscoveryPending, setIpListDiscoveryPending] = useState(false);
  const scanAbortController = useRef<AbortController>(new AbortController());

  const [foundMiners, setFoundMiners] = useState<Device[]>([]);

  const { refetch } = useOnboardingContext();

  const minerIds = useMinerIds();
  // Process discovered miners, ensuring no duplicates
  function processDiscoveredMiners(devices: Device[]) {
    setFoundMiners((prevMiners) => {
      const newMiners = devices.filter(
        (device) =>
          !prevMiners.some(
            (prevMiner) =>
              prevMiner.deviceIdentifier === device.deviceIdentifier,
          ) && !minerIds.some((minerId) => minerId === device.deviceIdentifier),
      );
      return [...prevMiners, ...newMiners];
    });
  }

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
    [discover],
  );

  const handleNmapDiscovery = useCallback(() => {
    if (!networkInfo) return;

    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "nmap",
        value: {
          target: networkInfo.subnet,
          ports: defaultDiscoveryPorts,
          fastScan: false,
        },
      },
    });
    setScanDiscoveryPending(true);
    handleDiscover(discoverRequest, scanAbortController.current).finally(() =>
      setScanDiscoveryPending(false),
    );
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
            timeoutSeconds: defaultTimeout,
          },
        },
      });
      setIpListDiscoveryPending(true);
      handleDiscover(discoverRequest).finally(() =>
        setIpListDiscoveryPending(false),
      );
    },
    [handleDiscover],
  );

  const handleRescan = useCallback(() => {
    // do not rescan if scan is already in progress
    if (!scanDiscoveryPending) {
      handleNmapDiscovery();
    }
  }, [scanDiscoveryPending, handleNmapDiscovery]);

  function handleContinue(selectedMinerIdentifiers: string[]) {
    const pairRequest = create(PairRequestSchema, {
      deviceIdentifiers: selectedMinerIdentifiers,
      // TODO DASH-476/add-credential-entry-screen: get credentials from user
      credentials: {
        username: "root",
        password: "root",
      },
    });
    pair({
      pairRequest: pairRequest,
      onSuccess: () => {
        refetch();
        if (mode === "onboarding") {
          navigate("/");
        } else {
          onExit?.();
          navigate(0);
        }
      },
      onError: (error) => {
        console.error("Pairing error:", error);
        pushToast({
          message: "Pairing failed",
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
