import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { action } from "storybook/actions";
import NavigationMenuComponent from ".";
import { primaryNavItems } from "@/protoFleet/config/navItems";

export const NavigationMenu = () => {
  return <NavigationMenuComponent items={primaryNavItems} isVisible={true} closeMenu={action("close menu")} />;
};

export default {
  title: "Proto Fleet/NavigationMenu",
  parameters: {
    withRouter: false,
  },
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
