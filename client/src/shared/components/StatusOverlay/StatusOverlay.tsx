import { motion } from "motion/react";
import { ReactNode } from "react";
import { useMemo } from "react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import ProgressCircular from "@/shared/components/ProgressCircular";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

type StatusOverlayProps = {
  text?: string;
  icon?: ReactNode;
};

const StatusOverlay = ({ text, icon = <LogoAlt width="w-16" /> }: StatusOverlayProps) => {
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
        <motion.div
          role="status"
          aria-label="Loading"
          animate={{ opacity: [0, 1] }}
          transition={{ duration: 0.5, delay: 0.5, ease: easeGentle }}
        >
          <ProgressCircular indeterminate />
        </motion.div>
        {text && (
          <motion.p
            animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="text-emphasis-300 text-text-primary-70"
          >
            {text}
          </motion.p>
        )}
      </div>
    </>
  );
};

export default StatusOverlay;
