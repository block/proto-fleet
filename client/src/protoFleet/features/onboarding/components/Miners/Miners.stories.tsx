import { useEffect, useState } from "react";
import { action } from "storybook/actions";
import MinersComponent from "./Miners";
import { createScaleDiscoveryMiners, scaleFleetNodes } from "./scaleDiscoveryFixture";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { useFleetStore } from "@/protoFleet/store";

type MinersProps = {
  minersCount: number;
};

export const Miners = ({ minersCount }: MinersProps) => {
  const [miners] = useState([
    ...Array.from(
      { length: 1000 },
      (_, i) =>
        ({
          $typeName: "pairing.v1.Device",
          fleetNodeId: 0n,
          macAddress: `0d:04:8a:54:fa:${(i + 10).toString(16).padStart(2, "0")}`,
          deviceIdentifier: `5440...88${(i + 10).toString().padStart(2, "0")}`,
        }) as Device,
    ),
  ]);

  return (
    <div>
      <MinersComponent
        foundMiners={miners.slice(0, minersCount)}
        scanDiscoveryPending={false}
        manualDiscoveryPending={false}
        pairingPending={false}
        onCancelScan={action("cancel scan")}
        onManualDiscover={() => null}
        onContinue={action("continue setup")}
        networkInfoPending={false}
        scanAvailable
        onRescan={action("rescan network")}
      />
    </div>
  );
};

export default {
  title: "Proto Fleet/Onboarding/Miners",
  args: {
    minersCount: 10,
  },
  argTypes: {
    minersCount: {
      control: {
        type: "range",
        min: 1,
        max: 1000,
        step: 1,
      },
    },
  },
};

export const FiftyThousandMiners = () => {
  const [miners] = useState(createScaleDiscoveryMiners);
  useEffect(() => {
    const previous = useFleetStore.getState().auth.permissions;
    useFleetStore.getState().auth.setPermissions(["miner:pair", "fleetnode:read"]);
    return () => useFleetStore.getState().auth.setPermissions(previous);
  }, []);
  return (
    <MinersComponent
      mode="pairing"
      initialStep="pairing"
      foundMiners={miners}
      fleetNodes={scaleFleetNodes}
      scanDiscoveryPending={false}
      manualDiscoveryPending={false}
      pairingPending={false}
      networkInfoPending={false}
      scanAvailable
      onCancelScan={action("cancel scan")}
      onManualDiscover={action("manual discovery")}
      onContinue={action("pair selected miners")}
      onRescan={action("rescan")}
    />
  );
};
