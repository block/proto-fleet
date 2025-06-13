import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { action } from "@storybook/addon-actions";
import NavigationMenuComponent from ".";
import routes from "@/protoFleet/routes";

export const NavigationMenu = () => {
  return (
    <NavigationMenuComponent
      routes={routes}
      isVisible={true}
      closeMenu={action("close menu")}
    />
  );
};

export default {
  title: "Components (ProtoFleet)/NavigationMenu",
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
