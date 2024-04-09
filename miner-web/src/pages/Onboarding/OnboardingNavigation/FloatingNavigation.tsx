import { useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import { usePreventScroll } from "common/hooks/usePreventScroll";

import { Tabs } from "../types";
import Navigation from "./Navigation";

interface FloatingNavigationProps {
  activeTab: Tabs;
  closeMenu?: () => void;
  poolUrls?: string[];
  onChangeActiveTab: (tab: Tabs) => void;
}

const FloatingNavigation = ({
  activeTab,
  closeMenu,
  poolUrls = [],
  onChangeActiveTab,
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
        className={clsx("z-30", {
          "animate-sliding-right": isVisible,
          "animate-sliding-left": !isVisible,
        })}
      >
        <Navigation
          activeTab={activeTab}
          poolUrls={poolUrls}
          onChangeActiveTab={onChangeActiveTab}
          onItemClick={handleCloseMenu}
        />
      </div>
    </div>
  );
};

export default FloatingNavigation;
