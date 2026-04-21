import { useCallback, useLayoutEffect, useState } from "react";
import clsx from "clsx";

import type { IpAddressInfoProps } from "./InfoItem/IpAddressInfo";
import type { MacAddressInfoProps } from "./InfoItem/MacAddressInfo";
import type { MinerNameInfoProps } from "./InfoItem/MinerNameInfo";
import type { VersionInfoProps } from "./InfoItem/VersionInfo";
import Navigation from "./Navigation";
import type { NavigationMenuType } from "./types";
import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

interface FloatingNavigationProps {
  closeMenu?: () => void;
  ipAddressInfo?: IpAddressInfoProps;
  macInfo?: MacAddressInfoProps;
  minerNameInfo?: MinerNameInfoProps;
  type: NavigationMenuType;
  versionInfo?: VersionInfoProps;
}

const FloatingNavigation = ({
  closeMenu,
  ipAddressInfo,
  macInfo,
  minerNameInfo,
  type,
  versionInfo,
}: FloatingNavigationProps) => {
  const [isVisible, setIsVisible] = useState(true);
  const { preventScroll } = usePreventScroll();
  useLayoutEffect(() => {
    preventScroll();
  }, [preventScroll]);

  const handleCloseMenu = useCallback(() => {
    setIsVisible(false);
    setTimeout(() => {
      closeMenu?.();
    }, 250);
  }, [closeMenu]);

  return (
    <div className="fixed z-20 h-screen bg-surface-elevated-base p-2">
      <button
        className={clsx("fixed top-0 left-0 z-20 h-screen w-screen bg-border-20 hover:cursor-default", {
          "animate-[fade-in_.3s_ease-in-out]": isVisible,
          "animate-[fade-out_.31s_ease-in-out]": !isVisible,
        })}
        onClick={handleCloseMenu}
      />
      <div
        className={clsx({
          "animate-[slide-right-nav_.3s_ease-in-out]": isVisible,
          "animate-[slide-left-nav_.3s_ease-in-out]": !isVisible,
        })}
      >
        <Navigation
          ipAddressInfo={ipAddressInfo}
          macInfo={macInfo}
          minerNameInfo={minerNameInfo}
          onItemClick={handleCloseMenu}
          versionInfo={versionInfo}
          type={type}
        />
      </div>
    </div>
  );
};

export default FloatingNavigation;
