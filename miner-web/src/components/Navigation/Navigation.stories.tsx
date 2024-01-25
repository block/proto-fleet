import React from "react";
import { MemoryRouter } from "react-router-dom";

import { getSerialNumbersDisplay, getUrlDisplay } from "common/utils/stringUtils";

import Navigation from ".";

export const NavigationSidebar = () => {
  const serials = getSerialNumbersDisplay([
    "1111111111111111111111",
    "2222222222222222222222",
    "3333333333333333333333",
  ]);
  const pool_url = getUrlDisplay("stratum+tcp://host.docker.internal:3333");
  // const pool_url = getUrlDisplay("stratum2+tcp://v2.stratum.braiins.com/u95GEReVMjK6k5YqiSFNqqTnKU4ypU2Wm8awa6tmbmDmk1bWt");
  return (
    <Navigation
      hashboard_serials={serials}
      controller_ip="210.1.1.0.0"
      controller_mac="0123456789101112131415"
      pool_info={{ status: "Alive", url: pool_url }}
    />
  );
};

export default {
  component: NavigationSidebar,
  title: "Navigation Sidebar",
  decorators: [
    (Story: React.ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
