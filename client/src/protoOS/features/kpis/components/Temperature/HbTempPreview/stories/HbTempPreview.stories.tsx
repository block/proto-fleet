import { type ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import HbTempPreviewComponent from "../HbTempPreview";
import { hbData } from "./mocks";

export const HbTempPreview = () => {
  // Component now uses serial and slot props instead of hbData
  // The actual data is fetched via the useMinerHashboard hook inside the component
  return <HbTempPreviewComponent serial={hbData.serial} slot={hbData.slot} />;
};

export default {
  title: "ProtoOS/HbTempPreview",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <div className="flex min-h-[100vh] w-full items-center justify-center">
          <Story />
        </div>
      </MemoryRouter>
    ),
  ],
};
