import { useState } from "react";
import { action } from "storybook/actions";
import MinersComponent from "./Miners";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

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
        ipListDiscoveryPending={false}
        pairingPending={false}
        onCancelScan={action("cancel scan")}
        onIpListModeDiscover={() => null}
        onContinue={action("continue setup")}
        onRescan={action("rescan network")}
        onClearFoundMiners={action("clear found miners")}
      />
    </div>
  );
};

export default {
  title: "ProtoFleet/Onboarding/Miners",
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
