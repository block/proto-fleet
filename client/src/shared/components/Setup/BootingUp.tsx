import { motion } from "motion/react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

interface BootingUpProps {
  title?: string;
}

const BootingUp = ({ title = "Your miner is booting up" }: BootingUpProps) => {
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  return (
    <div className="h-svh w-full">
      <AnimatedDotsBackground connecting>
        <div className="absolute top-1/2 left-1/2 z-10 flex h-[314px] w-[418px] -translate-x-1/2 -translate-y-1/2 flex-col items-center justify-center gap-6 rounded-3xl bg-white p-5 backdrop-blur-2xl dark:bg-white/5">
          <motion.div
            animate={{ color: ["#b3b3b3", `#000`], y: ["50%", "0%"] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="z-10"
          >
            <LogoAlt width="w-16" />
          </motion.div>
          <div className="grid duration-500">
            <motion.div
              animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
              exit={{ y: ["0%", "50%"], opacity: [1, 0] }}
              transition={{ duration: 1, ease: easeGentle }}
              className="col-start-1 row-start-1 flex flex-col items-center gap-6"
            >
              <p className="text-emphasis-300 text-text-primary-70">{title}</p>
            </motion.div>
          </div>
        </div>
      </AnimatedDotsBackground>
    </div>
  );
};

export default BootingUp;
