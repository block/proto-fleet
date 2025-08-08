import { useContext } from "react";
import { SystemContext } from "./SystemContext";

export const useSystemContext = () => {
  const context = useContext(SystemContext);
  if (context === undefined) {
    throw new Error(
      "useSystemContext must be used within a SystemContextProvider",
    );
  }
  return context;
};
