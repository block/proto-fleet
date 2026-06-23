import { useEffect } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { NotificationsContext } from "@/protoFleet/features/notifications/api/NotificationsContext";
import { useNotifications } from "@/protoFleet/features/notifications/api/useNotifications";
import ChannelsSection from "@/protoFleet/features/notifications/components/ChannelsSection";
import HistorySection from "@/protoFleet/features/notifications/components/HistorySection";
import MaintenanceWindowsSection from "@/protoFleet/features/notifications/components/MaintenanceWindowsSection";
import RulesSection from "@/protoFleet/features/notifications/components/RulesSection";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const Notifications = () => {
  const notifications = useNotifications();
  const { refresh } = notifications;

  useEffect(() => {
    void refresh().catch((error) => {
      pushToast({
        message: getErrorMessage(error, "Failed to load notifications"),
        status: STATUSES.error,
      });
    });
  }, [refresh]);

  return (
    <NotificationsContext.Provider value={notifications}>
      <div className="flex flex-col gap-6 pb-10">
        <Header title="Notifications" titleSize="text-heading-300" />
        <div className="flex flex-col gap-4">
          <RulesSection />
          <HistorySection />
          <ChannelsSection />
          <MaintenanceWindowsSection />
        </div>
      </div>
    </NotificationsContext.Provider>
  );
};

export default Notifications;
