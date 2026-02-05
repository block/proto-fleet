import { ReactNode, useLayoutEffect } from "react";
import clsx from "clsx";
import { createPortal } from "react-dom";

import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

interface PageOverlayProps {
  children: ReactNode;
  shouldPreventScroll?: boolean;
  show: boolean;
  zIndex?: string;
  animate?: boolean;
  position?: "top" | "center";
  className?: string;
}

const PageOverlay = ({
  children,
  shouldPreventScroll = true,
  show,
  animate = true,
  zIndex = "z-50",
  position = "center",
  className,
}: PageOverlayProps) => {
  const { preventScroll } = usePreventScroll();
  useLayoutEffect(() => {
    if (shouldPreventScroll) {
      preventScroll();
    }
  }, [preventScroll, shouldPreventScroll]);

  return (
    <>
      {createPortal(
        <div
          className={clsx(
            "fixed top-0 left-0 m-0! flex h-screen w-screen justify-center overflow-hidden! bg-grayscale-gray-5",
            zIndex,
            {
              "animate-[fade-in_.3s_ease-in-out]": animate && show,
              "animate-[fade-out_.31s_ease-in-out]": animate && !show,
              "items-center-safe p-0!": position === "center",
            },
            className,
          )}
        >
          {children}
        </div>,
        document.body,
      )}
    </>
  );
};

export default PageOverlay;
