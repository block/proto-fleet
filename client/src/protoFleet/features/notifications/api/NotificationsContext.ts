import { createContext, useContext } from "react";

import type { UseNotificationsResult } from "@/protoFleet/features/notifications/api/useNotifications";

export const NotificationsContext = createContext<UseNotificationsResult | null>(null);

export const useNotificationsContext = (): UseNotificationsResult => {
  const value = useContext(NotificationsContext);

  if (value === null) {
    throw new Error("useNotificationsContext must be used within a NotificationsContext.Provider");
  }

  return value;
};
