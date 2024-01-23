import React from "react";
import { MemoryRouter } from "react-router-dom";

import Navigation from ".";

export const NavigationSidebar = () => {
  return (
    <Navigation
      hashboard_serials={[
        "1111111111111111111111",
        "2222222222222222222222",
        "3333333333333333333333",
      ]}
      controller_ip="210.1.1.0.0"
      controller_mac="0123456789101112131415"
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
