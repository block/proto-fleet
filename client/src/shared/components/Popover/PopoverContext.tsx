import { createContext, MutableRefObject, ReactNode, useRef, useState } from "react";

type PopoverRenderMode = "inline" | "portal-fixed" | "portal-scrolling";

type PopoverContextType = {
  triggerRef: MutableRefObject<HTMLDivElement | null>;
  /**
   * Set how the popover should render.
   * - "inline": Render as child of trigger (best for desktop, no overflow issues)
   * - "portal-fixed": Render via portal with fixed positioning (for mobile with fixed headers)
   * - "portal-scrolling": Render via portal with absolute positioning (for scrolling containers)
   */
  setPopoverRenderMode: (mode: PopoverRenderMode) => void;
  /** @internal */
  renderMode: PopoverRenderMode;
};

const PopoverContext = createContext<PopoverContextType | null>(null);

type PopoverProviderProps = {
  children: ReactNode;
};

export const PopoverProvider = ({ children }: PopoverProviderProps) => {
  const triggerRef = useRef<HTMLDivElement>(null);
  const [renderMode, setPopoverRenderMode] = useState<PopoverRenderMode>("inline");

  return (
    <PopoverContext.Provider
      value={{
        triggerRef,
        setPopoverRenderMode,
        renderMode,
      }}
    >
      {children}
    </PopoverContext.Provider>
  );
};

export default PopoverContext;
