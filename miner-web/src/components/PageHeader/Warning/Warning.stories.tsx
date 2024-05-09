import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import WarningComponent from ".";

export const Warning = () => {
  return (
    <div className="flex space-x-4">
      <WarningComponent errorCount={47} errorType="asic" state="critical" />
      <WarningComponent errorCount={12} errorType="fan" state="warning" />
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
