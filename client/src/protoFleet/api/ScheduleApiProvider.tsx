import type { ReactNode } from "react";

import { ScheduleApiContext } from "@/protoFleet/api/ScheduleApiContext";
import useScheduleApi from "@/protoFleet/api/useScheduleApi";

export const ScheduleApiProvider = ({ children }: { children: ReactNode }) => {
  const scheduleApi = useScheduleApi();

  return <ScheduleApiContext.Provider value={scheduleApi}>{children}</ScheduleApiContext.Provider>;
};
