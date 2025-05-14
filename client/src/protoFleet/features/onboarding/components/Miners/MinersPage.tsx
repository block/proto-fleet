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
  }, [handleDiscover, networkInfo]);

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
    setFoundMiners([]);
  }

  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.miners}>
      <DialogComponent
        title="Pairing the found miners"
        subtitle="This may take a few seconds"
        loading
        show={pairingPending}
      />
      {discoverPending || foundMiners.length === 0 ? (
        <AddMiners
          loading={discoverPending}
          onScanModeDiscover={handleNmapDiscovery}
          onMdnsModeDiscover={handleMdnsDiscovery}
          onIpListModeDiscover={handleIpListDiscovery}
        />
      ) : (
        <FoundMiners
          miners={foundMiners}
          className="pt-0"
          handleContinueSetup={handleContinue}
          handleRestartSearch={handleRestart}
        />
      )}
    </OnboardingLayout>
  );
};

export default MinersPage;
