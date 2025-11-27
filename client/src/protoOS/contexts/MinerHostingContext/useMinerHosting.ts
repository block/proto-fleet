import { useContext, useMemo } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { api, minerRoot, closeButton } = useContext(MinerHostingContext);

  return useMemo(() => ({ api, minerRoot, closeButton }), [api, minerRoot, closeButton]);
};
