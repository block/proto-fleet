import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import Navigation from ".";

export const NavigationSidebar = () => {
  return (
    <Navigation
      macInfo={{ value: "42.08.59.58.84.c6" }}
      poolInfo={{
        status: "Alive",
        url: "stratum+tcp://host.docker.internal:3333",
      }}
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
