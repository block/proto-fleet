import { useContext } from "react";
import MinerHostingContext from "./MinerHostingContext";

export const useMinerHosting = () => {
  const { api, minerRoot, closeButton } = useContext(MinerHostingContext);

  return { api, minerRoot, closeButton };
};
