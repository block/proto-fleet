import { useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import { usePreventScroll } from "common/hooks/usePreventScroll";

import { MacAddressInfoProps } from "./MacAddressInfo";
import Navigation from "./Navigation";
import { VersionInfoProps } from "./VersionInfo";

interface FloatingNavigationProps {
  closeMenu?: () => void;
  macInfo?: MacAddressInfoProps;
  versionInfo?: VersionInfoProps;
}

const FloatingNavigation = ({
  closeMenu,
  macInfo,
  versionInfo,
}: FloatingNavigationProps) => {
  const [isVisible, setIsVisible] = useState(true);
  const { preventScroll } = usePreventScroll();
  useEffect(() => {
    preventScroll();
  }, [preventScroll]);

  const handleCloseMenu = useCallback(() => {
    setIsVisible(false);
    setTimeout(() => {
      closeMenu?.();
    }, 250);
  }, [closeMenu]);

  return (
    <div className="fixed h-screen bg-surface-base p-2 z-20">
      <button
        className={clsx(
          "fixed top-0 left-0 h-screen w-screen bg-border-primary/20 z-20 hover:cursor-default",
          {
            "animate-[fade-in_.3s_ease-in-out]": isVisible,
            "animate-[fade-out_.31s_ease-in-out]": !isVisible,
          }
        )}
        onClick={handleCloseMenu}
      />
      <div
        className={clsx({
          "animate-[slide-right-nav_.3s_ease-in-out]": isVisible,
          "animate-[slide-left-nav_.3s_ease-in-out]": !isVisible,
        })}
      >
        <Navigation
          macInfo={macInfo}
          onItemClick={handleCloseMenu}
          versionInfo={versionInfo}
        />
      </div>
    </div>
  );
};

export default FloatingNavigation;
