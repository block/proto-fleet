import { useEffect } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import ChannelsSection from "@/protoFleet/features/notifications/components/ChannelsSection";
import HistorySection from "@/protoFleet/features/notifications/components/HistorySection";
import RulesSection from "@/protoFleet/features/notifications/components/RulesSection";
import SilencesSection from "@/protoFleet/features/notifications/components/SilencesSection";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const Notifications = () => {
  // Single fetch on mount populates channels, rules, and silences in
  // parallel. Mutations from the section components hit the API and
  // merge the canonical row back into the cache, so we don't need to
  // re-fetch after each user action.
  const refresh = useNotificationsStore((s) => s.refresh);

  useEffect(() => {
    void refresh().catch((error) => {
      pushToast({
        message: getErrorMessage(error, "Failed to load notifications"),
        status: STATUSES.error,
      });
    });
  }, [refresh]);

  return (
    <div className="flex flex-col gap-6 pb-10">
      <Header title="Notifications" titleSize="text-heading-300" />
      <div className="flex flex-col gap-4">
        <RulesSection />
        <HistorySection />
        <ChannelsSection />
        <SilencesSection />
      </div>
    </div>
  );
};

export default Notifications;
