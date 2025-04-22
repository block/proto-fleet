import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { default as StoryComponent } from ".";
import routes from "@/protoFleet/routes";

export const SecondaryNavigation = () => {
  return <StoryComponent routes={routes} />;
};

export default {
  title: "Components (ProtoFleet)/SecondaryNavigation",
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
