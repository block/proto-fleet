import { useContext, useMemo } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { api, minerRoot, closeButton, mode, metadata } = useContext(MinerHostingContext);

  return useMemo(
    () => ({ api, minerRoot, closeButton, mode, metadata }),
    [api, minerRoot, closeButton, mode, metadata],
  );
};
