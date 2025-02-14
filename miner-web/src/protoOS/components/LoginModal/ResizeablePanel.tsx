import { AnimatePresence, motion } from "motion/react";
import { ReactNode } from "react";
import useMeasure from "@/shared/hooks/useMeasure";

import { cubicBezierValues } from "@/shared/utils/cssUtils";
import getTailwindConfig from "@/shared/utils/getTailwindConfig";

const gentle = getTailwindConfig("theme", "transitionTimingFunction", "gentle");
const gentleCb = cubicBezierValues(gentle);

type Props = {
  children: ReactNode,
  resizeOn: any
}

const ResizeablePanel = ({children, resizeOn}: Props) => {
  const [ref, { height }] = useMeasure<HTMLDivElement>();

  return (
    <motion.div
      initial={false}
      animate={{height}}
      transition={{duration: 0.3, ease: gentleCb}}
      className="relative"
    >
      <AnimatePresence>
        <motion.div 
          key={resizeOn}
          initial={{opacity: 0}}
          animate={{opacity: 1}}
          exit={{opacity: 0, transition: {duration: 0.2, ease: gentleCb}}}
          transition={{duration: 0.5, delay: 0.1, ease: gentleCb}}
        >
          <div ref={ref} className="absolute">{children}</div>
        </motion.div>
      </AnimatePresence>

    </motion.div>
  )
}

export default ResizeablePanel;