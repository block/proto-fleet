import ErrorModalComponent from "./ErrorModal";
import { FirmwareUpdateProvider } from "@/protoOS/features/firmwareUpdate";

export const ErrorModal = () => {
  return <ErrorModalComponent />;
};

export default {
  title: "protoOS/Firmware Update/ErrorModal",
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
