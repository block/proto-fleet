import { useEffect, useRef, useState } from "react";
import FoundMiners from "./FoundMiners";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { Success } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";

interface MinersProps {
  scanDiscoveryPending: boolean;
  ipListDiscoveryPending: boolean;
  pairingPending: boolean;
  foundMiners: Device[];
  onCancelScan: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
  onContinue: () => void;
  onRescan: () => void;
  onClearFoundMiners: () => void;
}

// Minimum time to show the loading animation in milliseconds (only for network scan)
const MIN_LOADING_TIME = 2000;

const Miners = ({
  scanDiscoveryPending,
  ipListDiscoveryPending,
  pairingPending,
  foundMiners,
  onCancelScan,
  onIpListModeDiscover,
  onContinue,
  onRescan,
  onClearFoundMiners,
}: MinersProps) => {
  const [deselectedMiners, setDeselectedMiners] = useState<
    Device["deviceIdentifier"][]
  >([]);
  const [selectedMode, setSelectedMode] = useState<string>(
    minerDiscoveryModes.scan,
  );
  const loadingTimeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [showScanLoading, setShowScanLoading] = useState(false);
  const [ipAddresses, setIpAddresses] = useState<string[]>([""]);

  // Handle loading state with minimum display time for network scan only
  useEffect(() => {
    if (scanDiscoveryPending) {
      setShowScanLoading(true);
    } else {
      loadingTimeoutId.current = setTimeout(() => {
        setShowScanLoading(false);
      }, MIN_LOADING_TIME);
    }

    return () => {
      if (loadingTimeoutId.current) {
        clearTimeout(loadingTimeoutId.current);
        loadingTimeoutId.current = null;
      }
    };
  }, [scanDiscoveryPending]);

  function handleIpAddressChange(newValue: string, index: number) {
    const newIpAddresses = [...ipAddresses];
    newIpAddresses[index] = newValue;

    if (newIpAddresses.filter((address) => address === "").length === 0) {
      setIpAddresses([...newIpAddresses, ""]);
    } else {
      setIpAddresses(newIpAddresses);
    }
  }

  function handleIpListDiscovery() {
    const validIpAddresses = ipAddresses.filter((address) => address !== "");
    if (validIpAddresses.length > 0) {
      onIpListModeDiscover(validIpAddresses);
    }
  }

  function handleScanCancel() {
    setShowScanLoading(false);
    if (loadingTimeoutId.current) {
      clearTimeout(loadingTimeoutId.current);
      loadingTimeoutId.current = null;
    }
    onCancelScan();
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
        onSelect={setSelectedMode}
      />

      {selectedMode === minerDiscoveryModes.scan && showScanLoading && (
        <div className="space-y-4">
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
          <div className="flex justify-end">
            <Button
              variant={variants.secondary}
              size={sizes.base}
              onClick={handleScanCancel}
            >
              Cancel scan
            </Button>
          </div>
        </div>
      )}

      {selectedMode === minerDiscoveryModes.ipList && (
        <div className="space-y-4">
          <div className="rounded-3xl border-1 border-core-primary-5 p-6">
            <div className="space-y-4">
              {ipAddresses.map((ipAddress, index) => (
                <Input
                  onChange={(value) => handleIpAddressChange(value, index)}
                  id={`ipAddress-${index}`}
                  key={`ipAddress-${index}`}
                  label="IP Address"
                  initValue={ipAddress}
                  statusIcon={
                    foundMiners.find(
                      (miner) => miner.ipAddress === ipAddress,
                    ) !== undefined ? (
                      <Success className="text-intent-success-fill" />
                    ) : undefined
                  }
                />
              ))}
            </div>
          </div>
          <div className="flex justify-end">
            <Button
              variant={variants.primary}
              size={sizes.base}
              loading={ipListDiscoveryPending}
              onClick={handleIpListDiscovery}
              disabled={ipAddresses.every((addr) => addr === "")}
            >
              Discover miners
            </Button>
          </div>
        </div>
      )}

      <FoundMiners
        className="mt-6"
        miners={foundMiners}
        deselectedMiners={deselectedMiners}
        setDeselectedMiners={setDeselectedMiners}
        minerDiscoveryMode={selectedMode}
        handleContinueSetup={onContinue}
        handleRescanNetwork={onRescan}
        handleClearMiners={onClearFoundMiners}
      />
    </div>
  );
};

export default Miners;
