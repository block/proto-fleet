import { ReactNode, useEffect } from "react";
import clsx from "clsx";

interface PageOverlayProps {
  children: ReactNode;
  preventScroll?: boolean;
  show: boolean;
  zIndex?: string;
}

const PageOverlay = ({
  children,
  preventScroll = true,
  show,
  zIndex = "z-20",
}: PageOverlayProps) => {
  useEffect(() => {
    if (preventScroll) {
      document.body.style.overflow = "hidden";
    }
    return () => {
      if (preventScroll) {
        document.body.style.overflow = "scroll";
      }
    };
  }, [preventScroll]);

  return (
    <div
      className={clsx(
        "fixed top-0 left-0 h-screen w-screen flex justify-center items-center bg-border-primary/5",
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
