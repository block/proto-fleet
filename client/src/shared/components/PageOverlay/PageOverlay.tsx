import { ReactNode, useEffect } from "react";
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
}

const PageOverlay = ({
  children,
  shouldPreventScroll = true,
  show,
  animate = true,
  zIndex = "z-50",
  position = "center",
}: PageOverlayProps) => {
  const { preventScroll } = usePreventScroll();
  useEffect(() => {
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
