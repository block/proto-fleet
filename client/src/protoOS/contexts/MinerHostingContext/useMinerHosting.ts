import { useContext, useMemo } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { api, adapter, minerRoot, closeButton, mode, metadata } = useContext(MinerHostingContext);

  return useMemo(
    () => ({ api, adapter, minerRoot, closeButton, mode, metadata, isFleetHosted: mode === "fleet" }),
    [api, adapter, minerRoot, closeButton, mode, metadata],
  );
};
