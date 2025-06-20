import InfoModalComponent from "./InfoModal";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate";

export const InfoModal = () => {
  return <InfoModalComponent closeModal={() => null} />;
};

export default {
  title: "protoOS/Firmware Update/InfoModal",
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
