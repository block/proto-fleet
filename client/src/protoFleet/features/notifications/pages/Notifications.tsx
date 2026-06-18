import ChannelsSection from "@/protoFleet/features/notifications/components/ChannelsSection";
import Header from "@/shared/components/Header";

const Notifications = () => {
  return (
    <div className="flex flex-col gap-6 pb-10">
      <Header title="Notifications" titleSize="text-heading-300" />
      <div className="flex flex-col gap-4">
        <ChannelsSection />
      </div>
    </div>
  );
};

export default Notifications;
