import { useEffect } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { AlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useAlerts } from "@/protoFleet/features/alerts/api/useAlerts";
import ChannelsSection from "@/protoFleet/features/alerts/components/ChannelsSection";
import HistorySection from "@/protoFleet/features/alerts/components/HistorySection";
import MaintenanceWindowsSection from "@/protoFleet/features/alerts/components/MaintenanceWindowsSection";
import RulesSection from "@/protoFleet/features/alerts/components/RulesSection";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const Alerts = () => {
  const alerts = useAlerts();
  const { refresh } = alerts;

  useEffect(() => {
    void refresh().catch((error) => {
      pushToast({
        message: getErrorMessage(error, "Failed to load alerts"),
        status: STATUSES.error,
      });
    });
  }, [refresh]);

  return (
    <AlertsContext.Provider value={alerts}>
      <div className="flex flex-col gap-6 pb-10">
        <Header title="Alerts" titleSize="text-heading-300" />
        <div className="flex flex-col gap-4">
          <RulesSection />
          <HistorySection />
          <ChannelsSection />
          <MaintenanceWindowsSection />
        </div>
      </div>
    </AlertsContext.Provider>
  );
};

export default Alerts;
