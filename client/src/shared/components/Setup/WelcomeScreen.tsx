import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

type NetworkInfoProps = {
  ipAddress: string;
  networkName: string;
};

const NetworkInfo = ({ ipAddress, networkName }: NetworkInfoProps) => {
  return (
    <>
      <div className="flex w-full justify-around">
        <div className="w-full">
          <Row>
            <div>Network</div>
          </Row>
        </div>
        <div>
          <Row>
            <div>{networkName}</div>
          </Row>
        </div>
      </div>
      <div className="flex w-full justify-around">
        <div className="w-full">
          <Row>
            <div>IP Address</div>
          </Row>
        </div>
        <div>
          <Row>
            <div>{ipAddress}</div>
          </Row>
        </div>
      </div>
    </>
  );
};

type WelcomeFlowProps = WelcomeScreenProps & {
  isBootstrapComplete: boolean;
};

const WelcomeFlow = ({
  searching,
  handleSearch,
  noMinersFound,
  handleRetry,
  networkName,
  ipAddress,
  isBootstrapComplete,
}: WelcomeFlowProps) => {
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);
  const [isReady, setIsReady] = useState(false);

  // Wait for bootstrap to complete before animating
  useEffect(() => {
    if (isBootstrapComplete && !isReady) {
      // Use requestIdleCallback to wait for the browser to be truly idle
      // This ensures all heavy API work and renders are complete
      const idleCallback = requestIdleCallback(
        () => {
          // Then use requestAnimationFrame for smooth animation start
          requestAnimationFrame(() => {
            setIsReady(true);
          });
        },
        { timeout: 500 }, // Fallback after 500ms if browser never becomes idle
      );

      return () => cancelIdleCallback(idleCallback);
    }
  }, [isBootstrapComplete, isReady]);

  if (!isReady) {
    return null;
  }

  return (
    <>
      {!noMinersFound ? (
        <div className="absolute top-1/2 left-1/2 z-10 flex h-[314px] w-[418px] -translate-x-1/2 -translate-y-1/2 flex-col items-center justify-center gap-6 bg-surface-base p-5 backdrop-blur-2xl">
          <motion.div
            initial={{ opacity: 0.2, y: "50%" }}
            animate={{ opacity: 1, y: "0%" }}
            transition={{ duration: 1, ease: easeGentle }}
            className="z-10"
          >
            <LogoAlt width="w-20" />
          </motion.div>
          <div className="grid h-[112px] duration-500">
            <AnimatePresence>
              {!searching ? (
                <motion.div
                  initial={{ y: "-50%", opacity: 0 }}
                  animate={{ y: "0%", opacity: 1 }}
                  exit={{ y: "50%", opacity: 0 }}
                  transition={{ duration: 1, ease: easeGentle }}
                  style={{ willChange: "transform, opacity" }}
                  className="col-start-1 row-start-1 flex flex-col items-center gap-6"
                >
                  <p className="text-5xl font-medium">Miner setup</p>
                  <Button onClick={handleSearch} variant="primary" size="base">
                    Get started
                  </Button>
                </motion.div>
              ) : null}
            </AnimatePresence>
            <AnimatePresence>
              {searching ? (
                ipAddress ? (
                  <motion.div
                    className="col-start-1 row-start-1 text-center"
                    initial={{ scale: 0, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ duration: 1, ease: easeGentle }}
                  >
                    <p className="text-text-primary-70">Connecting to your miner</p>
                    <p className="font-mono text-text-primary-70">{ipAddress}</p>
                  </motion.div>
                ) : (
                  <motion.div
                    initial={{ scale: 0, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ duration: 1, ease: easeGentle }}
                    className="text-center"
                  >
                    <p className="text-text-primary-70">Searching your network for miners</p>
                  </motion.div>
                )
              ) : null}
            </AnimatePresence>
          </div>
        </div>
      ) : null}
      <Modal
        open={noMinersFound}
        title="No Proto miners found"
        description="Ensure that your miner is plugged in with blinking LEDs and that
                it is connected to the network shown below."
        showHeader={false}
      >
        <div className="py-4">
          {/*TODO we dont have network name*/}
          <NetworkInfo ipAddress={ipAddress ?? ""} networkName={networkName ?? ""} />
        </div>
        <div className="flex flex-col gap-3">
          <Button onClick={handleRetry} variant="primary" size="base" className="w-full">
            Retry search
          </Button>
          <Button
            // TODO: Add support contact functionality
            variant="secondary"
            size="base"
            className="w-full"
          >
            Contact support
          </Button>
        </div>
      </Modal>
    </>
  );
};

type WelcomeScreenProps = {
  searching: boolean;
  handleSearch: () => void;
  noMinersFound: boolean;
  handleRetry: () => void;
  networkName?: string;
  ipAddress?: string;
  isBootstrapComplete?: boolean;
};

const WelcomeScreen = ({
  handleRetry,
  handleSearch,
  searching,
  noMinersFound,
  // TODO: Get network name from the miner
  networkName = "WBurg-Wifi-5G",
  ipAddress,
  isBootstrapComplete = true,
}: WelcomeScreenProps) => {
  return (
    <div className="h-svh w-full bg-surface-base">
      <AnimatedDotsBackground connecting={searching}>
        <WelcomeFlow
          searching={searching}
          handleSearch={handleSearch}
          handleRetry={handleRetry}
          noMinersFound={noMinersFound}
          networkName={networkName}
          ipAddress={ipAddress}
          isBootstrapComplete={isBootstrapComplete}
        />
      </AnimatedDotsBackground>
    </div>
  );
};

export default WelcomeScreen;
