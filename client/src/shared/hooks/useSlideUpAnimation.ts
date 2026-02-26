import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

const slideUpKeyframes = {
  initial: { opacity: 0, y: 24, scale: 0.98 },
  animate: { opacity: 1, y: 0, scale: 1 },
  exit: { opacity: 0, y: 24, scale: 0.98 },
} as const;

const useSlideUpAnimation = () => {
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  return {
    ...slideUpKeyframes,
    transition: { duration: 0.3, ease: easeGentle },
  };
};

export default useSlideUpAnimation;
