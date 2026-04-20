import { useCallback, useLayoutEffect, useState } from "react";
import clsx from "clsx";
import Navigation from "@/protoFleet/components/NavigationMenu/Navigation";
import { NavItem } from "@/protoFleet/config/navItems";
import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

type FloatingNavigationProps = {
  items: NavItem[];
  closeMenu?: () => void;
};

const FloatingNavigation = ({ items, closeMenu }: FloatingNavigationProps) => {
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
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Navigation menu"
      className="fixed z-20 h-screen bg-surface-elevated-base"
    >
      <button
        aria-label="Close navigation menu"
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
        <Navigation items={items} className="rounded-r-xl" closeMenu={handleCloseMenu} />
      </div>
    </div>
  );
};

export default FloatingNavigation;
