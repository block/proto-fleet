import { useContext, useEffect, useMemo } from "react";
import PopoverContext from "./PopoverContext";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export const usePopover = () => {
  const popoverContext = useContext(PopoverContext);
  if (popoverContext === null) throw new Error("usePopover must be used within a PopoverProvider");

  return useMemo(() => ({ ...popoverContext }), [popoverContext]);
};

/**
 * Hook for responsive popover rendering with fixed headers.
 * Automatically switches between inline rendering (desktop) and portal-fixed (mobile/tablet).
 *
 * Use this when your popover trigger is in a fixed header (like PageHeader).
 * On mobile/tablet, it uses portal rendering with fixed positioning to avoid overflow clipping.
 * On desktop, it uses inline rendering for better performance.
 *
 * @returns {Object} The popover context with triggerRef
 * @example
 * const { triggerRef } = useResponsivePopover();
 *
 * return (
 *   <div ref={triggerRef}>
 *     <button onClick={() => setOpen(true)}>Open</button>
 *     {open && <Popover />}
 *   </div>
 * );
 */
export const useResponsivePopover = () => {
  const { setPopoverRenderMode, ...rest } = usePopover();
  const { isPhone, isTablet } = useWindowDimensions();

  useEffect(() => {
    // On mobile/tablet with fixed PageHeader, use portal rendering with fixed positioning
    // On desktop, use inline rendering for better performance
    setPopoverRenderMode(isPhone || isTablet ? "portal-fixed" : "inline");
  }, [setPopoverRenderMode, isPhone, isTablet]);

  return rest;
};
