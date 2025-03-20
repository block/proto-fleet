import { motion } from "motion/react";
import { LogoAlt } from "@/shared/assets/icons";
import AnimatedDotsBackground from "@/shared/components/Animation";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

const BootingUp = () => {
  const easeGentle = useCssVariable({
    variable: "--ease-gentle",
    transform: cubicBezierValues,
  });

  return (
    <AnimatedDotsBackground connecting>
      <div className="absolute backdrop-blur-2xl  w-[418px] h-[314px] flex gap-6 flex-col justify-center items-center top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bg-white p-5 z-10">
        <motion.div
          animate={{ color: ["#b3b3b3", `#000`], y: ["50%", "0%"] }}
          transition={{ duration: 1, ease: easeGentle }}
          className="z-10"
        >
          <LogoAlt width="w-16" />
        </motion.div>
        <div className="grid duration-500 ">
          <motion.div
            animate={{ y: ["-50%", "0%"], opacity: [0, 1] }}
            exit={{ y: ["0%", "50%"], opacity: [1, 0] }}
            transition={{ duration: 1, ease: easeGentle }}
            className="flex flex-col gap-6 items-center col-start-1 row-start-1"
          >
            <p className="text-emphasis-300 text-text-primary-70">
              Your miner is booting up
            </p>
          </motion.div>
        </div>
      </div>
    </AnimatedDotsBackground>
  );
};

export default BootingUp;
