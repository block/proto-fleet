import { AnimatePresence, motion } from "motion/react";
import { ReactNode, useLayoutEffect } from "react";
import clsx from "clsx";
import { createPortal } from "react-dom";

import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

interface PageOverlayProps {
  children: ReactNode;
  open?: boolean;
  shouldPreventScroll?: boolean;
  zIndex?: string;
  position?: "top" | "center";
  className?: string;
}

interface PageOverlayContentProps {
  children: ReactNode;
  shouldPreventScroll: boolean;
  zIndex: string;
  position: "top" | "center";
  className?: string;
}

const PageOverlayContent = ({
  children,
  shouldPreventScroll,
  zIndex,
  position,
  className,
}: PageOverlayContentProps) => {
  const { preventScroll } = usePreventScroll();
  useLayoutEffect(() => {
    if (shouldPreventScroll) {
      preventScroll();
    }
  }, [preventScroll, shouldPreventScroll]);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.3, ease: "easeInOut" }}
      className={clsx(
        "fixed top-0 left-0 m-0! flex h-screen w-screen justify-center overflow-hidden! bg-grayscale-gray-5",
        zIndex,
        {
          "items-center-safe p-0!": position === "center",
        },
        className,
      )}
    >
      {children}
    </motion.div>
  );
};

const PageOverlay = ({
  children,
  open = true,
  shouldPreventScroll = true,
  zIndex = "z-50",
  position = "center",
  className,
}: PageOverlayProps) => {
  return createPortal(
    <AnimatePresence>
      {open ? (
        <PageOverlayContent
          shouldPreventScroll={shouldPreventScroll}
          zIndex={zIndex}
          position={position}
          className={className}
        >
          {children}
        </PageOverlayContent>
      ) : null}
    </AnimatePresence>,
    document.body,
  );
};

export default PageOverlay;
