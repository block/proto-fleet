import { AnimatePresence, motion } from "motion/react";
import { useState } from "react";
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

type WelcomeFlowProps = {
  searching: boolean;
  handleSearch: () => void;
  noMinersFound: boolean;
  handleRetry: () => void;
  networkName?: string;
  ipAddress?: string;
};

const WelcomeFlow = ({
  searching,
  handleSearch,
  noMinersFound,
  handleRetry,
  // TODO: Get network name from the miner
  networkName = "WBurg-Wifi-5G",
  // TODO: Get IP address from the miner
  ipAddress = "127.43.9424",
}: WelcomeFlowProps) => {
  const easeGentle = useCssVariable({
    variable: "--ease-gentle",
    transform: cubicBezierValues,
  });

  return (
    <>
      {!noMinersFound && (
        <div className="absolute top-1/2 left-1/2 z-10 flex h-[314px] w-[418px] -translate-x-1/2 -translate-y-1/2 flex-col items-center justify-center gap-6 bg-white p-5 backdrop-blur-2xl">
          <motion.div
            animate={{ color: ["#b3b3b3", `#000`], y: ["50%", "0%"] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="z-10"
          >
            <LogoAlt width="w-20" />
          </motion.div>
          <div className="grid h-[112px] duration-500">
            <AnimatePresence>
              {!searching && (
                <motion.div
                  animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
                  exit={{ y: ["0%", "50%"], opacity: [1, 0] }}
                  transition={{ duration: 1, ease: easeGentle }}
                  className="col-start-1 row-start-1 flex flex-col items-center gap-6"
                >
                  <p className="text-5xl font-medium">Miner setup</p>
                  <Button onClick={handleSearch} variant="accent" size="base">
                    Get started
                  </Button>
                </motion.div>
              )}
            </AnimatePresence>
            {searching &&
              (ipAddress ? (
                <motion.div
                  className="col-start-1 row-start-1 text-center"
                  animate={{ scale: [0, 1], opacity: [0, 1] }}
                  transition={{ duration: 1, ease: easeGentle }}
                >
                  <p className="text-text-primary-70">
                    Connecting to your miner
                  </p>
                  <p className="font-mono text-text-primary-70">{ipAddress}</p>
                </motion.div>
              ) : (
                <motion.div
                  animate={{ scale: [0, 1], opacity: [0, 1] }}
                  transition={{ duration: 1, ease: easeGentle }}
                  className="text-center"
                >
                  <p className="text-text-primary-70">
                    Searching your network for miners
                  </p>
                </motion.div>
              ))}
          </div>
        </div>
      )}
      {noMinersFound && (
        <Modal
          title="No Proto miners found"
          description="Ensure that your miner is plugged in with blinking LEDs and that
              it is connected to the network shown below."
          preventClose
          className="max-w-sm"
        >
          <div className="py-4">
            <NetworkInfo ipAddress={ipAddress} networkName={networkName} />
          </div>
          <div className="flex flex-col gap-3">
            <Button
              onClick={handleRetry}
              variant="primary"
              size="base"
              className="w-full"
            >
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
      )}
    </>
  );
};

const WelcomeScreen = () => {
  const [searching, setSearching] = useState(false);
  const [noMinersFound, setNoMinersFound] = useState(false);

  function handleSearch() {
    setSearching(true);

    // TODO: Replace with actual search logic
    setTimeout(() => {
      setSearching(false);
      setNoMinersFound(true);
    }, 5000);
  }

  function handleRetry() {
    setNoMinersFound(false);
    handleSearch();
  }

  return (
    <AnimatedDotsBackground connecting={searching}>
      <WelcomeFlow
        searching={searching}
        handleSearch={handleSearch}
        handleRetry={handleRetry}
        noMinersFound={noMinersFound}
      />
    </AnimatedDotsBackground>
  );
};

export default WelcomeScreen;
