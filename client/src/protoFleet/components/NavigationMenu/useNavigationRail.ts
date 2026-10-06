import { useEffect, useId, useRef, useState } from "react";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export function useNavigationRail() {
  const { isTablet, isLaptop } = useWindowDimensions();
  const isCollapsible = Boolean(isTablet || isLaptop);
  const [isExpanded, setIsExpanded] = useState(false);
  const navigationRef = useRef<HTMLElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const navigationId = useId();
  if (!isCollapsible && isExpanded) setIsExpanded(false);

  useEscapeDismiss(
    isExpanded
      ? () => {
          setIsExpanded(false);
          toggleRef.current?.focus();
        }
      : undefined,
  );

  useEffect(() => {
    if (!isExpanded) return;
    // Dismiss after the target handles its click. Collapsing on pointerdown
    // would move header controls before pointerup and could swallow their click.
    const handleClick = (event: MouseEvent) => {
      if (
        event.target instanceof Node &&
        !navigationRef.current?.contains(event.target) &&
        !toggleRef.current?.contains(event.target)
      ) {
        setIsExpanded(false);
      }
    };
    document.addEventListener("click", handleClick);
    return () => document.removeEventListener("click", handleClick);
  }, [isExpanded]);

  return {
    isCollapsible,
    isExpanded,
    navigationRef,
    toggleRef,
    navigationId,
    toggle: () => setIsExpanded((expanded) => !expanded),
    collapse: () => setIsExpanded(false),
  };
}

export type NavigationRail = ReturnType<typeof useNavigationRail>;
