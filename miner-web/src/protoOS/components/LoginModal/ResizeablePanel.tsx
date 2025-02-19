import { AnimatePresence, motion } from "motion/react";
import { ReactNode } from "react";
import useCssVariable from "@/shared/hooks/useCssVariable";
import useMeasure from "@/shared/hooks/useMeasure";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

type Props = {
  children: ReactNode,
  resizeOn: any
}

const ResizeablePanel = ({children, resizeOn}: Props) => {
  const [ref, { height }] = useMeasure<HTMLDivElement>();
  const easeGentle = useCssVariable({
    variable: "--ease-gentle",
    transform: cubicBezierValues,
  });

  return (
    <motion.div
      initial={false}
      animate={{height}}
      transition={{duration: 0.3}}
      className="relative"
    >
      <AnimatePresence>
        <motion.div 
          key={resizeOn}
          initial={{opacity: 0}}
          animate={{opacity: 1}}
          exit={{opacity: 0, transition: {duration: 0.2, ease: easeGentle}}}
          transition={{duration: 0.5, delay: 0.1}}
        >
          <div ref={ref} className="absolute">{children}</div>
        </motion.div>
      </AnimatePresence>

    </motion.div>
  )
}

export default ResizeablePanel;