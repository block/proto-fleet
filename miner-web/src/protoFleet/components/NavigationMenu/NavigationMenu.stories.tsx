import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { default as StoryComponent } from ".";
import routes from "@/protoFleet/routes";

export const NavigationMenu = () => {
  return <StoryComponent routes={routes} />;
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
