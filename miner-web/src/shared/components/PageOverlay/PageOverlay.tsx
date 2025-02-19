import { ReactNode, useEffect } from "react";
import clsx from "clsx";

import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

interface PageOverlayProps {
  children: ReactNode;
  shouldPreventScroll?: boolean;
  show: boolean;
  zIndex?: string;
}

const PageOverlay = ({
  children,
  shouldPreventScroll = true,
  show,
  zIndex = "z-40",
}: PageOverlayProps) => {
  const { preventScroll } = usePreventScroll();
  useEffect(() => {
    if (shouldPreventScroll) {
      preventScroll();
    }
  }, [preventScroll, shouldPreventScroll]);

  return (
    <div
      className={clsx(
        "fixed top-0 left-0 h-screen w-screen flex justify-center items-center bg-grayscale-gray-5 m-0! p-0! overflow-hidden!",
        zIndex,
        {
          "animate-[fade-in_.3s_ease-in-out]": show,
          "animate-[fade-out_.31s_ease-in-out]": !show,
        }
      )}
    >
      {children}
    </div>
  );
};

export default PageOverlay;
