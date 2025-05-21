import { AnimatePresence, motion } from "motion/react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { cubicBezier } from "motion";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import NetworkConfirmationDialog from "@/protoFleet/features/onboarding/components/Welcome/NetworkConfirmationDialog";
import { Logo } from "@/shared/assets/icons";
import LandingPageBgImage from "@/shared/assets/images/landing_page_bg.png";
import BackgroundImage from "@/shared/components/BackgroundImage";
import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import { THEMES } from "@/shared/features/preferences/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const order = {
  logo: 3,
  copy: 1,
  buttons: 2,
  footer: 3,
};

const WelcomePage = () => {
  const { isPhone, isTablet } = useWindowDimensions();
  const navigate = useNavigate();

  const { data: networkInfo } = useNetworkInfo();
  const [showNetworkDialog, setShowNetworkDialog] = useState(false);

  const openInNewTab = (url: string) => {
    window.open(url, "_blank");
  };

  const itemAnimationVariants = {
    hidden: () => ({
      opacity: 0,
      y: "20px",
    }),
    visible: (order: number) => ({
      opacity: 1,
      y: 0,
      transition: {
        delay: order * 0.2,
        duration: 1.0 - order * 0.2,
        ease: cubicBezier(0.25, 0.46, 0.45, 0.94),
      },
    }),
    exit: () => ({
      opacity: 0,
      y: 0,
      transition: {
        duration: 0.6,
        ease: cubicBezier(0.25, 0.46, 0.45, 0.94), // easeOutQuad
      },
    }),
  };

  const handleSetup = () => {
    setShowNetworkDialog(true);
  };

  const handleContinue = () => {
    setShowNetworkDialog(false);
    navigate("/onboarding/auth");
  };

  const handleCancel = () => {
    setShowNetworkDialog(false);
  };

  return (
    <BackgroundImage
      className="h-screen"
      image={LandingPageBgImage}
      backgroundPosition={
        isPhone ? "60% center" : isTablet ? "70% center" : undefined
      }
    >
      <AnimatePresence mode="wait">
        {showNetworkDialog ? (
          <motion.div
            key="network-confirmation-dialog"
            animate={{ opacity: [0, 1] }}
            exit={{ opacity: [1, 0] }}
            transition={{ duration: 0.3, ease: "easeInOut" }}
          >
            <NetworkConfirmationDialog
              subnet={networkInfo?.subnet}
              gateway={networkInfo?.gateway}
              show={true}
              onCancel={handleCancel}
              onConfirm={handleContinue}
            />
          </motion.div>
        ) : (
          <div
            key="welcome-page"
            className="container mx-auto h-screen py-6 phone:w-88 tablet:w-146 laptop:w-160 desktop:w-160"
            data-theme={THEMES.dark}
          >
            <motion.div
              className="flex h-full flex-col justify-between"
              initial="hidden"
              animate="visible"
              exit="exit"
            >
              <motion.div variants={itemAnimationVariants} custom={order.logo}>
                <Logo className="text-text-primary" width="w-[86px]" />
              </motion.div>
              <div>
                <motion.div
                  variants={itemAnimationVariants}
                  custom={order.copy}
                >
                  <Header
                    title="ProtoFleet"
                    titleSize="text-display-300"
                    description="Manage and monitor your mining fleet."
                  />
                </motion.div>
                <motion.div
                  variants={itemAnimationVariants}
                  custom={order.buttons}
                >
                  <ButtonGroup
                    className="mt-6 space-x-4"
                    variant={groupVariants.leftAligned}
                    size={sizes.base}
                    buttons={[
                      {
                        text: "Set up a new fleet",
                        onClick: handleSetup,
                        variant: variants.accent,
                      },
                      {
                        text: "Log in",
                        onClick: () => navigate("/auth"),
                        variant: variants.secondary,
                        className: "bg-grayscale-white-20",
                      },
                    ]}
                  />
                </motion.div>
              </div>
              <motion.div
                className="flex flex-row items-end justify-between"
                variants={itemAnimationVariants}
                custom={order.footer}
              >
                <div className="flex flex-col text-200 text-text-primary">
                  <div>Powerful mining tools.</div>
                  <div>Built for decentralization.</div>
                  <div className="text-text-primary-30">
                    © {new Date().getFullYear()} Block, Inc. Privacy Notice
                  </div>
                </div>
                <ButtonGroup
                  className="flex-wrap space-y-3 phone:justify-end phone:space-x-0"
                  variant={groupVariants.rightAligned}
                  size={sizes.compact}
                  buttons={[
                    {
                      text: "API Documentation",
                      // TODO use ProtoFleet API docs
                      onClick: () =>
                        openInNewTab(
                          "https://proto.xyz/docs/api/v1.1.0/#api-_",
                        ),
                      variant: variants.ghost,
                      className: "border border-grayscale-gray-15",
                    },
                    {
                      text: "Support",
                      // TODO send mail?
                      onClick: () =>
                        openInNewTab(
                          "https://www.mining.build/manuals/hardware/miner-assembly/",
                        ),
                      variant: variants.ghost,
                      className: "border border-grayscale-gray-15",
                    },
                  ]}
                />
              </motion.div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </BackgroundImage>
  );
};

export default WelcomePage;
