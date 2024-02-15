import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "@storybook/addon-actions";

import Navigation from ".";

export const NavigationSidebar = () => {
  return (
    <Navigation
      hashboard_serials={{
        value: [
          "1111111111111111111111",
          "2222222222222222222222",
          "3333333333333333333333",
        ],
      }}
      controller_ip={{ value: "210.1.1.0.0" }}
      controller_mac={{ value: "42.08.59.58.84.c6" }}
      pool_info={{
        status: "Alive",
        url: "stratum+tcp://host.docker.internal:3333",
      }}
      onClickReboot={() => action("Reboot")()}
      onClickSleep={() => action("Sleep")()}
    />
  );
};

export default {
  component: NavigationSidebar,
  title: "Navigation Sidebar",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
