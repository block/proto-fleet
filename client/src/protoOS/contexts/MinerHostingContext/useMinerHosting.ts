import { useContext, useMemo } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { adapter, resolution, minerRoot, closeButton, mode, metadata } = useContext(MinerHostingContext);

  return useMemo(
    () => ({ adapter, resolution, minerRoot, closeButton, mode, metadata, isFleetHosted: mode === "fleet" }),
    [adapter, resolution, minerRoot, closeButton, mode, metadata],
  );
};
