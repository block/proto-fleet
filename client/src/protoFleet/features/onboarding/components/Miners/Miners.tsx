import { useState } from "react";
import FoundMiners from "./FoundMiners";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

interface MinersProps {
  loading: boolean;
  pairingPending: boolean;
  foundMiners: Device[];
  onScanModeDiscover: () => void;
  onMdnsModeDiscover: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
  onContinue: () => void;
  onRestart: () => void;
}

const Miners = ({
  loading,
  pairingPending,
  foundMiners,
  onScanModeDiscover,
  onMdnsModeDiscover,
  onIpListModeDiscover,
  onContinue,
  onRestart,
}: MinersProps) => {
  const [deselectedMiners, setDeselectedMiners] = useState<
    Device["deviceIdentifier"][]
  >([]);
  const [selectedMode, setSelectedMode] = useState<string>(
    minerDiscoveryModes.scan,
  );
  const [ipAddresses, setIpAddresses] = useState<string[]>([""]);

  const handleSelect = (selectedKey: string) => {
    setSelectedMode(selectedKey);
    switch (selectedKey) {
      case minerDiscoveryModes.scan:
        onScanModeDiscover();
        break;
      case minerDiscoveryModes.mdns:
        onMdnsModeDiscover();
        break;
      default:
        break;
    }
  };

  function handleIpAddressChange(newValue: string, index: number) {
    const newIpAddresses = [...ipAddresses];
    newIpAddresses[index] = newValue;

    if (newIpAddresses.filter((address) => address === "").length === 0) {
      setIpAddresses([...newIpAddresses, ""]);
    } else {
      setIpAddresses(newIpAddresses);
    }
  }

  return (
    <div>
      <Dialog
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
      <SegmentedControl
        className="my-4"
        segments={[
          {
            key: minerDiscoveryModes.scan,
            title: "Scan network",
          },
          {
            key: minerDiscoveryModes.ipList,
            title: "Specify IP addresses",
          },
        ]}
        onSelect={handleSelect}
      />

      {selectedMode !== minerDiscoveryModes.ipList &&
        (loading ? (
          <div className="grow rounded-3xl border-1 border-core-primary-5">
            <div className="p-6">
              <h2 className="text-heading-200">Scanning your network</h2>
              <p className="text-300 text-text-primary-70">
                This may take a few seconds.
              </p>
            </div>
            <div className="h-74 px-6 pb-6">
              <AnimatedDotsBackground connecting={true} padding={0} />
            </div>
          </div>
        ) : (
          <FoundMiners
            miners={foundMiners}
            deselectedMiners={deselectedMiners}
            setDeselectedMiners={setDeselectedMiners}
            handleContinueSetup={onContinue}
            handleRestartSearch={onRestart}
          />
        ))}

      {selectedMode === minerDiscoveryModes.ipList && (
        <div className="space-y-4">
          {ipAddresses.map((ipAddress, index) => (
            <Input
              onChange={(value) => handleIpAddressChange(value, index)}
              id={`ipAddress-${index}`}
              key={`ipAddress-${index}`}
              label="IP Address"
              initValue={ipAddress}
            />
          ))}
          <Button
            variant={variants.primary}
            size={sizes.base}
            loading={loading}
            onClick={() =>
              onIpListModeDiscover(
                ipAddresses.filter((address) => address !== ""),
              )
            }
          >
            Discover miners
          </Button>
        </div>
      )}
    </div>
  );
};

export default Miners;
