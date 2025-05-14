import { ReactNode, useState } from "react";
import AnimatedDotsBackground from "../Animation";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

interface AddMinerProps {
  loading: boolean;
  onScanModeDiscover: () => void;
  onMdnsModeDiscover: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
  scanResults: ReactNode;
}

const AddMiners = ({
  loading,
  onScanModeDiscover,
  onMdnsModeDiscover,
  onIpListModeDiscover,
  scanResults,
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
          scanResults
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

export default AddMiners;
