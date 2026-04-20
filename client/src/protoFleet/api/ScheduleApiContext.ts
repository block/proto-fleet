import { createContext, useContext } from "react";

import type { UseScheduleApiResult } from "@/protoFleet/api/useScheduleApi";

export const ScheduleApiContext = createContext<UseScheduleApiResult | null>(null);

export const useScheduleApiContext = () => {
  const scheduleApi = useContext(ScheduleApiContext);

  if (scheduleApi === null) {
    throw new Error("useScheduleApiContext must be used within a ScheduleApiProvider");
  }

  return scheduleApi;
};
