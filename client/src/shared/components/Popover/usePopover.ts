import { useContext, useMemo } from "react";
import PopoverContext from "./PopoverContext";

export const usePopover = () => {
  const popoverContext = useContext(PopoverContext);
  if (popoverContext === null)
    throw new Error("usePopover must be used within a PopoverProvider");

  return useMemo(() => ({ ...popoverContext }), [popoverContext]);
};
