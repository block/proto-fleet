import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "@storybook/addon-actions";

import Navigation from ".";

export const NavigationSidebar = () => {
  return (
    <Navigation
      hashboardSerials={{
        value: [
          "1111111111111111111111",
          "2222222222222222222222",
          "3333333333333333333333",
        ],
      }}
      controllerIp={{ value: "210.1.1.0.0" }}
      controllerMac={{ value: "42.08.59.58.84.c6" }}
      poolInfo={{
        status: "Alive",
        url: "stratum+tcp://host.docker.internal:3333",
      }}
      onClickReboot={() => action("Reboot")()}
      onClickSleep={() => action("Sleep")()}
    />
  );
};

export default {
  title: "Navigation Sidebar",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
