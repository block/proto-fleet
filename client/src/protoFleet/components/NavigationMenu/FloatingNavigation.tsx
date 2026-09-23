import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";
import Navigation from "@/protoFleet/components/NavigationMenu/Navigation";
import { NavItem } from "@/protoFleet/config/navItems";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { usePreventScroll } from "@/shared/hooks/usePreventScroll";

type FloatingNavigationProps = {
  items: NavItem[];
  closeMenu?: () => void;
};

const FloatingNavigation = ({ items, closeMenu }: FloatingNavigationProps) => {
  const [isVisible, setIsVisible] = useState(true);
  const dialogRef = useRef<HTMLDivElement>(null);
  const { preventScroll } = usePreventScroll();
  useLayoutEffect(() => {
    preventScroll();
    const trigger = document.activeElement;
    dialogRef.current?.querySelector<HTMLElement>("nav a")?.focus();
    return () => {
      if (trigger instanceof HTMLElement && trigger.isConnected) trigger.focus();
    };
  }, [preventScroll]);

  const handleCloseMenu = useCallback(() => {
    setIsVisible(false);
  }, []);
  useEscapeDismiss(handleCloseMenu);

  useEffect(() => {
    if (isVisible) return;
    const timeout = setTimeout(() => closeMenu?.(), 300);
    return () => clearTimeout(timeout);
  }, [isVisible, closeMenu]);

  return (
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label="Navigation menu"
      className="fixed inset-0 z-50 h-dvh"
      onKeyDown={(event) => {
        if (event.key !== "Tab") return;
        const controls = dialogRef.current?.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]):not([tabindex="-1"])',
        );
        if (!controls?.length) return;
        const first = controls[0];
        const last = controls[controls.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }}
    >
      <button
        aria-label="Close navigation menu"
        tabIndex={-1}
        className={clsx("fixed top-0 left-0 z-20 h-dvh w-screen bg-border-20 hover:cursor-default", {
          "animate-[fade-in_.3s_ease-in-out]": isVisible,
          "animate-[fade-out_.31s_ease-in-out]": !isVisible,
        })}
        onClick={handleCloseMenu}
      />
      <div
        className={clsx("relative z-30", {
          "animate-[slide-right-nav_.3s_ease-in-out]": isVisible,
          "animate-[slide-left-nav_.3s_ease-in-out]": !isVisible,
        })}
      >
        <Navigation items={items} className="rounded-r-xl" closeMenu={handleCloseMenu} isFloatingMenu />
      </div>
    </div>
  );
};

export default FloatingNavigation;
