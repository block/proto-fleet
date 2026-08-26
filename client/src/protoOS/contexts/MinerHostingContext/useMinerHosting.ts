import { useContext, useMemo } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { api, adapter, resolution, minerRoot, closeButton, mode, metadata } = useContext(MinerHostingContext);

  return useMemo(
    () => ({ api, adapter, resolution, minerRoot, closeButton, mode, metadata, isFleetHosted: mode === "fleet" }),
    [api, adapter, resolution, minerRoot, closeButton, mode, metadata],
  );
};
