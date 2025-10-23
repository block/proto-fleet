import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { default as StoryComponent } from ".";
import { secondaryNavItems } from "@/protoFleet/config/navItems";

export const SecondaryNavigation = () => {
  return <StoryComponent items={secondaryNavItems} />;
};

export default {
  title: "Proto Fleet/SecondaryNavigation",
  args: {},
  argTypes: {},
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter initialEntries={["/settings/general"]}>
        <Story />
      </MemoryRouter>
    ),
  ],
};
