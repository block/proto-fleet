import type { ReactNode } from "react";
import { MaintenanceOptionsContext, useLoadMaintenanceOptions } from "../hooks/useMaintenanceOptions";

const MaintenanceOptionsProvider = ({ children }: { children: ReactNode }) => {
  const options = useLoadMaintenanceOptions();
  return <MaintenanceOptionsContext.Provider value={options}>{children}</MaintenanceOptionsContext.Provider>;
};

export default MaintenanceOptionsProvider;
