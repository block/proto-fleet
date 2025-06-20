import UpdateAvailableComponent from "./UpdateAvailable";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate";

export const UpdateAvailable = () => {
  return <UpdateAvailableComponent dismiss={() => {}} />;
};

export default {
  title: "protoOS/Firmware Update/UpdateAvailable",
  decorators: [
    (Story: any) => (
      <FirmwareUpdateProvider>
        <div className="flex min-h-[100vh] w-full items-center justify-center px-16">
          <div className="w-[240px]">
            <Story />
          </div>
        </div>
      </FirmwareUpdateProvider>
    ),
  ],
};
