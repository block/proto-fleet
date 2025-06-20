import InstallModalComponent from "./InstallModal";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate";

export const InstallModal = () => {
  return <InstallModalComponent closeModal={() => null} />;
};

export default {
  title: "protoOS/Firmware Update/InstallModal",
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
