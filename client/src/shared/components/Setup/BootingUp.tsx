import { motion } from "motion/react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import ProgressCircular from "@/shared/components/ProgressCircular";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

interface BootingUpProps {
  title?: string;
}

const BootingUp = ({ title }: BootingUpProps) => {
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  return (
    <div className="h-svh w-full bg-surface-base">
      <AnimatedDotsBackground connecting>
        <div className="absolute top-1/2 left-1/2 z-10 flex h-[314px] w-[418px] -translate-x-1/2 -translate-y-1/2 flex-col items-center justify-center gap-6 rounded-3xl bg-white p-5 backdrop-blur-2xl dark:bg-white/5">
          <motion.div
            animate={{ color: ["#b3b3b3", `#000`], y: ["50%", "0%"] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="z-10"
          >
            <LogoAlt width="w-16" />
          </motion.div>
          <motion.div
            role="status"
            aria-label="Loading"
            animate={{ opacity: [0, 1] }}
            transition={{ duration: 0.5, delay: 0.5, ease: easeGentle }}
          >
            <ProgressCircular indeterminate />
          </motion.div>
          {title && (
            <motion.p
              animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
              transition={{ duration: 1, ease: easeGentle }}
              className="text-emphasis-300 text-text-primary-70"
            >
              {title}
            </motion.p>
          )}
        </div>
      </AnimatedDotsBackground>
    </div>
  );
};

export default BootingUp;
