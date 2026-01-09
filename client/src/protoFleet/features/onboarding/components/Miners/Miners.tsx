import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import FoundMiners from "./FoundMiners";
import FoundMinersModal from "./FoundMinersModal";
import { MinerDiscoveryMode } from "./types";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { Dismiss, LogoAlt } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import { minerDiscoveryModes } from "@/shared/components/Setup/miners.constants";
import Textarea from "@/shared/components/Textarea";

interface MinersProps {
  scanDiscoveryPending: boolean;
  ipListDiscoveryPending: boolean;
  pairingPending: boolean;
  foundMiners: Device[];
  onCancelScan: () => void;
  onIpListModeDiscover: (ipAddresses: string[]) => void;
  onContinue: (selectedMinerIdentifiers: string[]) => void;
  onRescan: () => void;
  onClearFoundMiners: () => void;
  mode?: MinerDiscoveryMode;
}

// Minimum time to show the loading animation in milliseconds (only for network scan)
const MIN_LOADING_TIME = 2000;

// Parse IP addresses from text value which can contain newlines and commas
function parseIpList(input: string): string[] {
  return input
    .split(/[\n,]+/)
    .map((addr) => addr.trim())
    .filter((addr) => addr !== "");
}

const Miners = ({
  scanDiscoveryPending,
  ipListDiscoveryPending,
  pairingPending,
  foundMiners,
  onCancelScan,
  onIpListModeDiscover,
  onContinue,
  onRescan,
  mode = "onboarding",
}: MinersProps) => {
  const [deselectedMiners, setDeselectedMiners] = useState<Device["deviceIdentifier"][]>([]);
  const [selectedMode] = useState<string>(minerDiscoveryModes.scan);
  const loadingTimeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [showScanLoading, setShowScanLoading] = useState(false);
  const [textareaValue, setTextareaValue] = useState<string>("");
  const [showModal, setShowModal] = useState(false);
  const [showFoundMinersModal, setShowFoundMinersModal] = useState(false);
  const [activeStep, setActiveStep] = useState<"findMiners" | "pairing">("findMiners");

  // Handle loading state with minimum display time for network scan only
  useEffect(() => {
    if (scanDiscoveryPending) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
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

  function handleIpAddressChange(newValue: string) {
    setTextareaValue(newValue);
  }

  function handleIpListDiscovery() {
    const parsedAddresses = parseIpList(textareaValue);

    // Send valid addresses to the discovery function
    if (parsedAddresses.length > 0) {
      onIpListModeDiscover(parsedAddresses);
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
    <div className="h-[calc(100vh-theme(spacing.1)*15)] p-6 sm:p-10">
      <Dialog title="Pairing the found miners" subtitle="This may take a few seconds" loading show={pairingPending} />

      {mode === "onboarding" && (
        <div className="flex h-full w-full items-center rounded-xl bg-landing-page p-6 sm:p-20 dark:bg-core-primary-5">
          <div className="flex flex-col gap-12">
            <div className="flex flex-col gap-4">
              <LogoAlt width="w-[48px]" />
              <Header
                title="Let's setup your fleet."
                titleSize="text-display-200"
                description="Add miners to your fleet to get started."
              />
            </div>
            <div>
              <Button variant="primary" onClick={() => setShowModal(true)}>
                Get started
              </Button>
            </div>
          </div>
        </div>
      )}

      {(mode === "pairing" || showModal) && (
        <PageOverlay show>
          <div className="h-full w-full overflow-auto bg-surface-base p-6">
            <Header
              className="sticky top-0 z-10 pb-14"
              title="Add miners"
              titleSize="text-heading-200"
              icon={<Dismiss />}
              iconOnClick={
                pairingPending
                  ? undefined
                  : () => {
                      handleScanCancel();
                      setActiveStep("findMiners");
                      setShowModal(false);
                    }
              }
              inline
              buttonSize={sizes.base}
              buttons={
                showScanLoading
                  ? []
                  : [
                      {
                        variant: variants.secondary,
                        onClick: onRescan,
                        text: "Rescan network",
                        disabled: pairingPending,
                        className: clsx({
                          hidden: activeStep !== "pairing" || selectedMode !== minerDiscoveryModes.scan,
                        }),
                      },
                      {
                        variant: variants.secondary,
                        onClick: () => {
                          setShowFoundMinersModal(true);
                        },
                        text: "Choose miners",
                        disabled: pairingPending,
                        className: clsx({
                          hidden: activeStep !== "pairing" || foundMiners.length <= 1,
                        }),
                      },
                      {
                        variant: variants.primary,
                        loading: pairingPending,
                        onClick: () => {
                          const selectedMinerIdentifiers = foundMiners
                            .filter((miner) => !deselectedMiners.includes(miner.deviceIdentifier))
                            .map((miner) => miner.deviceIdentifier);
                          onContinue(selectedMinerIdentifiers);
                        },
                        disabled:
                          pairingPending || foundMiners.length === 0 || foundMiners.length === deselectedMiners.length,
                        text: pairingPending
                          ? `Adding ${foundMiners.length - deselectedMiners.length} miners...`
                          : `Continue with ${foundMiners.length - deselectedMiners.length} miners`,
                        className: clsx({
                          hidden: activeStep !== "pairing",
                        }),
                      },
                    ]
              }
            />
            {activeStep === "findMiners" && (
              <div className="mx-auto max-w-4xl">
                <Header
                  title="Miners"
                  description={
                    <>
                      <p>
                        Scan your network or provide miner IP addresses and hostnames to find miners to add to your
                        fleet.
                      </p>
                      <p>Note that you can add more miners and adjust security settings after setup.</p>
                    </>
                  }
                  titleSize="text-heading-300"
                  inline
                />

                <div className="my-6 flex flex-col gap-4 rounded-3xl border-1 border-core-primary-5 p-6">
                  <Header
                    inline
                    title="Scan your network"
                    titleSize="text-heading-200"
                    description="Scan your network to find miners to add to your fleet or provide miner IP addresses and hostnames to find miners to add to your fleet.."
                  />
                  <div>
                    <Button
                      variant={variants.primary}
                      onClick={() => {
                        setActiveStep("pairing");
                        onRescan();
                      }}
                      size={sizes.base}
                      loading={scanDiscoveryPending}
                    >
                      Find miners
                    </Button>
                  </div>
                </div>

                <div className="flex flex-col gap-4 rounded-3xl border-1 border-core-primary-5 p-6">
                  <Header
                    inline
                    title="Enter network info manually"
                    titleSize="text-heading-200"
                    description="Add your IP addresses and/or hostnames, separated by commas and/or line breaks (if pasting from a spreadsheet). Example: 192.168.1.10, miner01, 192.168.1.11, miner02, etc"
                  />
                  <div>
                    <div className="space-y-4">
                      <Textarea
                        onChange={(value) => handleIpAddressChange(value)}
                        initValue={textareaValue}
                        id="ipAddresses"
                        label="IP Addresses"
                      />
                    </div>
                  </div>
                  <div>
                    <Button
                      variant={variants.secondary}
                      size={sizes.base}
                      loading={ipListDiscoveryPending}
                      onClick={() => {
                        setActiveStep("pairing");
                        setShowModal(true);
                        handleIpListDiscovery();
                      }}
                      disabled={!textareaValue.trim()}
                    >
                      Find miners
                    </Button>
                  </div>
                </div>
              </div>
            )}
            {activeStep === "pairing" && (
              <div className="mx-auto max-w-4xl">
                {showScanLoading ? (
                  <>
                    <Header
                      title="Finding miners on your network"
                      titleSize="text-heading-300"
                      inline
                      className="mb-6"
                    />
                    <div className="flex flex-col gap-5">
                      {Array.from({ length: 3 }).map((_, index) => (
                        <div key={index} className="flex items-center justify-between">
                          <div className="flex items-center gap-4">
                            <div className="size-5 animate-pulse rounded-full bg-core-primary-20"></div>
                            <div className="flex flex-col gap-3">
                              <div className="h-3 w-24 animate-pulse rounded-sm bg-core-primary-20"></div>
                              <div className="h-3 w-60 animate-pulse rounded-sm bg-core-primary-20"></div>
                            </div>
                          </div>
                          <div className="h-3 w-12 animate-pulse rounded-sm bg-core-primary-20"></div>
                        </div>
                      ))}
                    </div>
                  </>
                ) : (
                  <>
                    <FoundMiners miners={foundMiners} deselectedMiners={deselectedMiners} className="" />
                    {showFoundMinersModal && (
                      <FoundMinersModal
                        setDeselectedMiners={setDeselectedMiners}
                        miners={foundMiners.map((miner) => ({
                          ...miner,
                          selected: !deselectedMiners.includes(miner.deviceIdentifier),
                        }))}
                        models={Array.from(new Set(foundMiners.map((miner) => miner.model)))}
                        onDismiss={() => setShowFoundMinersModal(false)}
                      />
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        </PageOverlay>
      )}
    </div>
  );
};

export default Miners;
