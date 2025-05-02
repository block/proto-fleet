import { useState } from "react";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

interface AddMinerProps {
  onScanModeDiscover: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
}

const AddMiners = ({
  onScanModeDiscover,
  onIpListModeDiscover,
}: AddMinerProps) => {
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
    <div className="container mx-auto max-w-[640px]">
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

export default AddMiners;
