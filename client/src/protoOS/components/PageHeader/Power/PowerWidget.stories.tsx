import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "storybook/actions";

import PowerWidgetComponent from "./PowerWidget";
import { PopoverProvider } from "@/shared/components/Popover";

export const PowerWidget = () => {
  const handleReboot = () => {
    action("rebooting")();
  };

  const handleSleep = () => {
    action("sleeping")();
  };

  const handleWake = () => {
    action("waking up")();
  };

  return (
    <div className="flex w-96 justify-end">
      <PopoverProvider>
        <PowerWidgetComponent shouldShowPopover onReboot={handleReboot} onSleep={handleSleep} onWake={handleWake} />
      </PopoverProvider>
    </div>
  );
};

export default {
  title: "protoOS/Page Header/Power Widget",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
