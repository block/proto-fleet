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
import DialogComponent from "@/shared/components/Dialog";
import { AddMiners, FoundMiners, SetupHeader } from "@/shared/components/Setup";
import {
  protoFleetSteps,
  steps,
} from "@/shared/components/Setup/setupHeader.constants";
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

  const handleScanNetwork = useCallback(() => {
    if (!networkInfo) return;

    const discoverRequest = create(DiscoverRequestSchema, {
      mode: {
        case: "ipRange",
        value: {
          startIp: networkInfo.localIp,
          // TODO fix endIp
          endIp: "192.168.2.255",
          // TODO where to get ports?
          ports: ["8080", "2121", "2122", "4100", "4200"],
          timeoutSeconds: 10,
        },
      },
    });
    handleDiscover(discoverRequest);
  }, [handleDiscover, networkInfo]);

  useEffect(() => {
    handleScanNetwork();
  }, [handleScanNetwork]);

  const handleDiscoverIpList = useCallback(
    (ipAddresses: string[]) => {
      const discoverRequest = create(DiscoverRequestSchema, {
        mode: {
          case: "ipList",
          value: {
            ipAddresses: ipAddresses,
            // TODO where to get ports?
            ports: ["8080", "2121", "2122", "4100", "4200"],
            timeoutSeconds: 10,
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
        navigate("/onboarding/network");
      },
      onError: (error) => {
        // TODO handle error
        console.error("Pairing error:", error);
      },
    });
  }

  return (
    <div>
      <SetupHeader steps={protoFleetSteps} activeStep={steps.miners} />
      <AddMiners
        onScanModeDiscover={handleScanNetwork}
        onIpListModeDiscover={handleDiscoverIpList}
      />
      <DialogComponent
        title="Pairing the found miners"
        subtitle="This may take a few seconds"
        loading
        show={pairingPending}
      />
      {!discoverPending && foundMiners.length > 0 && (
        <FoundMiners
          miners={foundMiners}
          handleContinueSetup={handleContinue}
        />
      )}
    </div>
  );
};

export default MinersPage;
