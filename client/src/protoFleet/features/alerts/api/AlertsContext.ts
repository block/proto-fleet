import { createContext, useContext } from "react";

import type { UseAlertsResult } from "@/protoFleet/features/alerts/api/useAlerts";

export const AlertsContext = createContext<UseAlertsResult | null>(null);

export const useAlertsContext = (): UseAlertsResult => {
  const value = useContext(AlertsContext);

  if (value === null) {
    throw new Error("useAlertsContext must be used within a AlertsContext.Provider");
  }

  return value;
};
