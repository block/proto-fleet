import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import FoundMiners from "./FoundMiners";
import FoundMinersModal from "./FoundMinersModal";
import { MinerDiscoveryMode } from "./types";
import ValidationErrorDialog from "./ValidationErrorDialog";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import NullState from "@/protoFleet/components/NullState";
import { Dismiss, LogoAlt } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import PageOverlay from "@/shared/components/PageOverlay";
import Textarea from "@/shared/components/Textarea";
import { CategorizedInvalidEntries, ManualDiscoveryTargets, parseManualTargets } from "@/shared/utils/networkDiscovery";

interface MinersProps {
  scanDiscoveryPending: boolean;
  ipListDiscoveryPending: boolean;
  pairingPending: boolean;
  networkInfoPending: boolean;
  scanAvailable: boolean;
  foundMiners: Device[];
  onCancelScan: () => void;
  onManualDiscover: (targets: ManualDiscoveryTargets) => void;
  onContinue: (selectedMinerIdentifiers: string[]) => void;
  onRescan: () => void;
  onForemanImport?: (apiKey: string, clientId: string) => void;
  foremanImportPending?: boolean;
  mode?: MinerDiscoveryMode;
}

// Minimum time to show the loading animation in milliseconds (only for network scan)
const MIN_LOADING_TIME = 2000;

const Miners = ({
  scanDiscoveryPending,
  ipListDiscoveryPending,
  pairingPending,
  networkInfoPending,
  scanAvailable,
  foundMiners,
  onCancelScan,
  onManualDiscover,
  onContinue,
  onRescan,
  onForemanImport,
  foremanImportPending = false,
  mode = "onboarding",
}: MinersProps) => {
  const [deselectedMiners, setDeselectedMiners] = useState<Device["deviceIdentifier"][]>([]);
  const loadingTimeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [showScanLoading, setShowScanLoading] = useState(false);
  const [textareaValue, setTextareaValue] = useState<string>("");
  const [ipListError, setIpListError] = useState<string | boolean>(false);
  const [showModal, setShowModal] = useState(false);
  const [showFoundMinersModal, setShowFoundMinersModal] = useState(false);
  const [activeStep, setActiveStep] = useState<"findMiners" | "pairing">("findMiners");
  const [showValidationErrorDialog, setShowValidationErrorDialog] = useState(false);
  const [categorizedInvalidEntries, setCategorizedInvalidEntries] = useState<CategorizedInvalidEntries | null>(null);
  const [pendingValidTargets, setPendingValidTargets] = useState<ManualDiscoveryTargets | null>(null);
  const [showForemanModal, setShowForemanModal] = useState(false);
  const [foremanApiKey, setForemanApiKey] = useState("");
  const [foremanClientId, setForemanClientId] = useState("");

  const discoveryPending = scanDiscoveryPending || ipListDiscoveryPending;
  const showLoadingSkeleton = showScanLoading || discoveryPending;
  const displayMiners = useMemo(() => {
    const seen = new Set<string>();

    return foundMiners.filter((miner) => {
      const identity = miner.ipAddress || miner.deviceIdentifier;
      if (!identity) {
        return true;
      }
      if (seen.has(identity)) {
        return false;
      }
      seen.add(identity);
      return true;
    });
  }, [foundMiners]);
  const selectedDisplayMiners = displayMiners.filter((miner) => !deselectedMiners.includes(miner.deviceIdentifier));

  // Handle loading state with minimum display time
  useEffect(() => {
    if (discoveryPending) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- show loading immediately when discovery starts; minimum display time enforced on hide
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
  }, [discoveryPending]);

  function handleIpAddressChange(newValue: string) {
    setTextareaValue(newValue);
    if (ipListError) {
      setIpListError(false);
    }
  }

  function handleManualDiscovery() {
    const { targets, invalidEntries, categorizedInvalidEntries: categorized } = parseManualTargets(textareaValue);
    const hasTargets = targets.ipAddresses.length + targets.subnets.length + targets.ipRanges.length > 0;

    if (!hasTargets && invalidEntries.length === 0) {
      setIpListError("Enter at least one IP address, hostname, subnet, or IP range.");
      return false;
    }

    if (!hasTargets && invalidEntries.length > 0) {
      setCategorizedInvalidEntries(categorized);
      setPendingValidTargets(null);
      setShowValidationErrorDialog(true);
      return false;
    }

    if (invalidEntries.length > 0) {
      setCategorizedInvalidEntries(categorized);
      setPendingValidTargets(targets);
      setShowValidationErrorDialog(true);
      return false;
    }

    setIpListError(false);
    onManualDiscover(targets);
    return true;
  }

  function handleBackToEditing() {
    setShowValidationErrorDialog(false);

    if (categorizedInvalidEntries) {
      const allInvalid = [
        ...categorizedInvalidEntries.ipAddresses,
        ...categorizedInvalidEntries.ipRanges,
        ...categorizedInvalidEntries.subnets,
      ].join(", ");

      setIpListError(`Check the format of the following and retry:\n${allInvalid}`);
    }

    setCategorizedInvalidEntries(null);
    setPendingValidTargets(null);
  }

  function handleContinueAnyway() {
    setShowValidationErrorDialog(false);
    setCategorizedInvalidEntries(null);

    if (pendingValidTargets) {
      setIpListError(false);
      onManualDiscover(pendingValidTargets);
      setActiveStep("pairing");
      setShowModal(true);
    }

    setPendingValidTargets(null);
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
    <div className="h-[calc(100vh-theme(spacing.1)*15)]">
      <Dialog open={pairingPending} title="Pairing the found miners" subtitle="This may take a few seconds" loading />

      {mode === "onboarding" && (
        <NullState
          icon={<LogoAlt width="w-5" />}
          title="Let's setup your fleet."
          description="Add miners to your fleet to get started."
          action={
            <Button variant="primary" onClick={() => setShowModal(true)}>
              Get started
            </Button>
          }
        />
      )}

      <PageOverlay open={mode === "pairing" || showModal} zIndex="z-50">
        <div className="h-full w-full overflow-auto bg-surface-base p-6">
          <Header
            className="sticky top-0 z-10 pb-14"
            title="Add miners"
            titleSize="text-heading-300"
            {...(pairingPending
              ? {
                  icon: <Dismiss />,
                }
              : {
                  icon: <Dismiss />,
                  iconAriaLabel: "Close add miners",
                  iconOnClick: () => {
                    handleScanCancel();
                    setActiveStep("findMiners");
                    setShowModal(false);
                  },
                })}
            inline
            buttons={
              showLoadingSkeleton && displayMiners.length === 0
                ? []
                : [
                    {
                      variant: variants.secondary,
                      onClick: () => {
                        setDeselectedMiners([]);
                        onRescan();
                      },
                      text: discoveryPending ? "Scanning" : "Rescan network",
                      disabled: pairingPending || discoveryPending,
                      loading: discoveryPending,
                      className: clsx({
                        hidden: activeStep !== "pairing",
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
                        hidden: activeStep !== "pairing" || displayMiners.length <= 1,
                      }),
                    },
                    {
                      variant: variants.primary,
                      loading: pairingPending,
                      onClick: () => {
                        const selectedMinerIdentifiers = selectedDisplayMiners.map((miner) => miner.deviceIdentifier);
                        onContinue(selectedMinerIdentifiers);
                      },
                      disabled: pairingPending || selectedDisplayMiners.length === 0,
                      text: pairingPending
                        ? `Adding ${selectedDisplayMiners.length} miners...`
                        : `Continue with ${selectedDisplayMiners.length} miners`,
                      className: clsx({
                        hidden: activeStep !== "pairing" || displayMiners.length === 0,
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
                      Scan your network or provide miner IP addresses and hostnames to find miners to add to your fleet.
                    </p>
                    <p>Note that you can add more miners and adjust security settings after setup.</p>
                  </>
                }
                titleSize="text-heading-300"
                inline
              />

              <div className={clsx("my-6 grid gap-4", onForemanImport ? "grid-cols-2" : "grid-cols-1")}>
                <div
                  className="flex flex-col gap-4 rounded-3xl bg-core-primary-5 p-6"
                  data-testid="section-scan-network"
                >
                  <Header
                    inline
                    title="Scan your network"
                    titleSize="text-heading-200"
                    description="Scan your network to find miners to add to your fleet or provide miner IP addresses and hostnames to find miners to add to your fleet."
                  />
                  <div>
                    <Button
                      variant={variants.primary}
                      onClick={() => {
                        setDeselectedMiners([]);
                        setActiveStep("pairing");
                        onRescan();
                      }}
                      size={sizes.base}
                      loading={scanDiscoveryPending || networkInfoPending}
                      disabled={!scanAvailable}
                    >
                      Find miners
                    </Button>
                  </div>
                </div>

                {onForemanImport && (
                  <div
                    className="flex flex-col gap-4 rounded-3xl bg-core-primary-5 p-6"
                    data-testid="section-import-foreman"
                  >
                    <Header
                      inline
                      title="Import from Foreman"
                      titleSize="text-heading-200"
                      description="Connect your Foreman account to import your existing miners, including pool configuration and group assignments."
                    />
                    <div>
                      <Button variant={variants.primary} size={sizes.base} onClick={() => setShowForemanModal(true)}>
                        Connect Foreman
                      </Button>
                    </div>
                  </div>
                )}
              </div>

              <div className="flex flex-col gap-4 rounded-3xl bg-core-primary-5 p-6" data-testid="section-search-by-ip">
                <Header
                  inline
                  title="Enter network info manually"
                  titleSize="text-heading-200"
                  description="Add your IP addresses and/or hostnames, IP ranges, or subnets. Separate entries with commas or line breaks. Examples: 10.32.1.100, 192.168.1.0/24, 10.32.1.100 - 10.32.1.150"
                />
                <div>
                  <div className="space-y-4">
                    <Textarea
                      onChange={(value) => handleIpAddressChange(value)}
                      initValue={textareaValue}
                      id="ipAddresses"
                      testId="ipAddresses"
                      label="IP Addresses"
                      error={ipListError}
                    />
                  </div>
                </div>
                <div>
                  <Button
                    variant={variants.secondary}
                    size={sizes.base}
                    loading={ipListDiscoveryPending}
                    onClick={() => {
                      const shouldProceed = handleManualDiscovery();
                      if (shouldProceed) {
                        setActiveStep("pairing");
                        setShowModal(true);
                      }
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
              <FoundMiners
                miners={displayMiners}
                deselectedMiners={deselectedMiners}
                className=""
                isScanning={discoveryPending}
                showSkeleton={showLoadingSkeleton}
              />
              {displayMiners.length > 0 && (
                <FoundMinersModal
                  open={showFoundMinersModal}
                  setDeselectedMiners={setDeselectedMiners}
                  miners={displayMiners.map((miner) => ({
                    ...miner,
                    selected: !deselectedMiners.includes(miner.deviceIdentifier),
                  }))}
                  models={Array.from(new Set(displayMiners.map((miner) => miner.model)))}
                  onDismiss={() => setShowFoundMinersModal(false)}
                />
              )}
            </div>
          )}
        </div>
      </PageOverlay>

      <Modal
        open={showForemanModal}
        title="Import from Foreman"
        description="Connect your Foreman account to import miners"
        onDismiss={() => setShowForemanModal(false)}
        buttons={[
          {
            text: foremanImportPending ? "Importing..." : "Import",
            variant: variants.primary,
            onClick: () => {
              if (onForemanImport && foremanApiKey && foremanClientId) {
                onForemanImport(foremanApiKey, foremanClientId);
                setForemanApiKey("");
                setForemanClientId("");
                setShowForemanModal(false);
                setActiveStep("pairing");
                setShowModal(true);
              }
            },
            disabled: !foremanApiKey || !foremanClientId || foremanImportPending,
            loading: foremanImportPending,
          },
        ]}
      >
        <div className="flex flex-col gap-4 p-4">
          <Input
            id="foremanApiKey"
            label="API key"
            type="password"
            initValue={foremanApiKey}
            onChange={(value) => setForemanApiKey(value)}
          />
          <Input
            id="foremanClientId"
            label="Client ID"
            initValue={foremanClientId}
            onChange={(value) => setForemanClientId(value)}
          />
        </div>
      </Modal>

      <ValidationErrorDialog
        open={showValidationErrorDialog && !!categorizedInvalidEntries}
        invalidEntries={categorizedInvalidEntries ?? { ipAddresses: [], ipRanges: [], subnets: [] }}
        hasValidEntries={pendingValidTargets !== null}
        onBackToEditing={handleBackToEditing}
        onContinueAnyway={handleContinueAnyway}
      />
    </div>
  );
};

export default Miners;
