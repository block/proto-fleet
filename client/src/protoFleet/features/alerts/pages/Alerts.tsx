import { useEffect } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { AlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { useAlerts } from "@/protoFleet/features/alerts/api/useAlerts";
import ChannelsSection from "@/protoFleet/features/alerts/components/ChannelsSection";
import HistorySection from "@/protoFleet/features/alerts/components/HistorySection";
import MaintenanceWindowsSection from "@/protoFleet/features/alerts/components/MaintenanceWindowsSection";
import RulesSection from "@/protoFleet/features/alerts/components/RulesSection";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ALERTS_PAGE_DESCRIPTION = "Configure alert rules, notification channels, and maintenance windows.";

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
        <SettingsPageHeader title="Alerts" description={ALERTS_PAGE_DESCRIPTION} />
        <div className="flex flex-col gap-4">
          <RulesSection />
          <MaintenanceWindowsSection />
          <ChannelsSection />
          <HistorySection />
        </div>
      </div>
    </AlertsContext.Provider>
  );
};

export default Alerts;
