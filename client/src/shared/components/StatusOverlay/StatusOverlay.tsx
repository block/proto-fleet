import { motion } from "motion/react";
import { ReactNode } from "react";
import { useMemo } from "react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

type StatusOverlayProps = {
  text?: string;
  icon?: ReactNode;
};

const StatusOverlay = ({
  text = "Your miner is booting up",
  icon = <LogoAlt width="w-16" />,
}: StatusOverlayProps) => {
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  // Memoize the AnimatedDotsBackground to prevent resetting on re-renders
  const animatedDots = useMemo(() => <AnimatedDotsBackground connecting />, []);

  return (
    <>
      {animatedDots}
      <div className="absolute top-1/2 left-1/2 z-10 flex h-[314px] w-[418px] -translate-x-1/2 -translate-y-1/2 flex-col items-center justify-center gap-6 rounded-3xl bg-surface-base p-5 backdrop-blur-2xl">
        <motion.div
          animate={{ opacity: [0, 1], y: ["50%", "0%"] }}
          transition={{ duration: 1, ease: easeGentle }}
          className="z-10"
        >
          {icon}
        </motion.div>
        <div className="grid duration-500">
          <motion.div
            animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
            exit={{ y: ["0%", "50%"], opacity: [1, 0] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="col-start-1 row-start-1 flex flex-col items-center gap-6"
          >
            <p className="text-emphasis-300 text-text-primary-70">{text}</p>
          </motion.div>
        </div>
      </div>
    </>
  );
};

export default StatusOverlay;
