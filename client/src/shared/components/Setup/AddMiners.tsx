import { useState } from "react";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

interface AddMinerProps {
  loading: boolean;
  onScanModeDiscover: () => void;
  onMdnsModeDiscover: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
}

const AddMiners = ({
  loading,
  onScanModeDiscover,
  onMdnsModeDiscover,
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
            key: minerDiscoveryModes.mdns,
            title: "mDNS scan",
          },
          {
            key: minerDiscoveryModes.ipList,
            title: "Specify IP addresses",
          },
        ]}
        onSelect={handleSelect}
      />
      {loading && selectedMode !== minerDiscoveryModes.ipList && (
        <div className="flex grow items-center space-x-3">
          <ProgressCircular indeterminate />
          <span className="text-emphasis-300">Discovery in progress...</span>
        </div>
      )}
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

export default AddMiners;
