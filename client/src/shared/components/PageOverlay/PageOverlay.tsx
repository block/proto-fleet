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
}

const PageOverlay = ({
  children,
  shouldPreventScroll = true,
  show,
  animate = true,
  zIndex = "z-50",
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
            "fixed top-0 left-0 m-0! flex h-screen w-screen items-center justify-center overflow-hidden! bg-grayscale-gray-5 p-0!",
            zIndex,
            {
              "animate-[fade-in_.3s_ease-in-out]": animate && show,
              "animate-[fade-out_.31s_ease-in-out]": animate && !show,
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
