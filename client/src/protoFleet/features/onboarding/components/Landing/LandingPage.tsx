import { motion } from "motion/react";
import { useNavigate } from "react-router-dom";
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
import useCssVariable from "@/shared/hooks/useCssVariable";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

const order = {
  logo: 3,
  copy: 1,
  buttons: 2,
  footer: 3,
};

const LandingPage = () => {
  const { isPhone, isTablet } = useWindowDimensions();
  const navigate = useNavigate();
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  const openInNewTab = (url: string) => {
    window.open(url, "_blank");
  };

  const itemAnimationVariants = {
    hidden: { opacity: 0, y: "20px" },
    visible: (order: number) => ({
      opacity: 1,
      y: 0,
      transition: { delay: order * 0.2, duration: 0.4, ease: easeGentle },
    }),
  };

  return (
    <BackgroundImage
      image={LandingPageBgImage}
      backgroundPosition={
        isPhone ? "60% center" : isTablet ? "70% center" : undefined
      }
    >
      <div
        className="container mx-auto h-screen py-6 phone:w-88 tablet:w-146 laptop:w-160 desktop:w-160"
        data-theme={THEMES.dark}
      >
        <motion.div
          className="flex h-full flex-col justify-between"
          variants={{
            hidden: { opacity: 0 },
            visible: { opacity: 1 },
          }}
          initial="hidden"
          animate="visible"
        >
          <motion.div variants={itemAnimationVariants} custom={order.logo}>
            <Logo className="text-text-primary" width="w-[86px]" />
          </motion.div>
          <div>
            <motion.div variants={itemAnimationVariants} custom={order.copy}>
              <Header
                title="ProtoFleet"
                titleSize="text-display-300"
                description="Manage and monitor your mining fleet."
              />
            </motion.div>
            <motion.div variants={itemAnimationVariants} custom={order.buttons}>
              <ButtonGroup
                className="mt-6 space-x-4"
                variant={groupVariants.leftAligned}
                size={sizes.base}
                buttons={[
                  {
                    text: "Set up a new fleet",
                    onClick: () => navigate("/onboarding"),
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
                    openInNewTab("https://proto.xyz/docs/api/v1.1.0/#api-_"),
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
    </BackgroundImage>
  );
};

export default LandingPage;
