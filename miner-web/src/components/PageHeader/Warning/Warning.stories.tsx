import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import WarningComponent from ".";

export const Warning = () => {
  return (
    <div className="flex space-x-4">
      <WarningComponent
        label="ASIC"
        state="critical"
        messages={["12% Higher Temperature"]}
      />
      <WarningComponent
        label="Fans"
        state="warning"
        messages={["Fan 1 low speed", "Fan 2 low speed"]}
      />
    </div>
  );
};

export default {
  title: "Components/Page Header/Warning",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
