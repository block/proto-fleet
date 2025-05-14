import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  Device,
  DiscoverRequest,
  DiscoverRequestSchema,
  PairRequestSchema,
} from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useMinerPairing } from "@/protoFleet/api/useMinerPairing";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import {
  defaultDiscoveryPorts,
  defaultTimeout,
  STEP_KEYS,
  STEPS,
} from "@/protoFleet/features/onboarding/constants";
import DialogComponent from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import {
  AddMiners,
  FoundMiners,
  OnboardingLayout,
} from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const MinersPage = () => {
  const navigate = useNavigate();
  const { data: networkInfo } = useNetworkInfo();

  const { discoverPending, discover, pairingPending, pair } = useMinerPairing();
  const [foundMiners, setFoundMiners] = useState<Device[]>([]);
  const [rescan, setRescan] = useState<boolean>(false);

  function processDiscoveredMiners(devices: Device[]) {
    setFoundMiners((prevMiners) => [...prevMiners, ...devices]);
  }

  const handleDiscover = useCallback(
    (discoverRequest: DiscoverRequest) => {
      setFoundMiners([]);

      discover({
        discoverRequest: discoverRequest,
        onStreamData: processDiscoveredMiners,
        onError: (error) => {
          // TODO handle error
          console.error("Discovery error:", error);
        },
      });
    },
    [discover],
  );

  /*const handleIPRangeDiscovery = useCallback(() => {
    if (!networkInfo) return;

    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "ipRange",
        value: {
          startIp: networkInfo.localIp,
          // TODO fix endIp
          endIp: "192.168.2.255",
          ports: defaultDiscoveryPorts,
          timeoutSeconds: defaultTimeout,
        },
      },
    });
    handleDiscover(discoverRequest);
  }, [handleDiscover, networkInfo]);*/

  const handleNmapDiscovery = useCallback(() => {
    void rescan;
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
    handleDiscover(discoverRequest);
  }, [handleDiscover, networkInfo, rescan]);

  useEffect(() => {
    handleNmapDiscovery();
  }, [handleNmapDiscovery]);

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
      handleDiscover(discoverRequest);
    },
    [handleDiscover],
  );

  function handleContinue() {
    const pairRequest = create(PairRequestSchema, {
      deviceIdentifiers: foundMiners.map((device) => device.deviceIdentifier),
    });
    pair({
      pairRequest: pairRequest,
      onSuccess: () => {
        navigate("/onboarding/security");
      },
      onError: (error) => {
        // TODO handle error
        console.error("Pairing error:", error);
      },
    });
  }

  function handleRestart() {
    setRescan((prev) => !prev);
  }

  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.miners}>
      <DialogComponent
        title="Pairing the found miners"
        subtitle="This may take a few seconds"
        loading
        show={pairingPending}
      />
      <Header
        title="Add miners"
        titleSize="text-heading-300"
        description="Scan your network or upload a list of miner IP addresses to add them to your fleet."
        inline
      />
      <AddMiners
        loading={discoverPending}
        onScanModeDiscover={handleNmapDiscovery}
        onMdnsModeDiscover={handleMdnsDiscovery}
        onIpListModeDiscover={handleIpListDiscovery}
        scanResults={
          <FoundMiners
            miners={foundMiners}
            handleContinueSetup={handleContinue}
            handleRestartSearch={handleRestart}
          />
        }
      />
    </OnboardingLayout>
  );
};

export default MinersPage;
