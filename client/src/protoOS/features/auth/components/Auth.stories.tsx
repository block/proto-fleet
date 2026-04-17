import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import Auth from "./Auth";

export const SignUp = () => {
  return <Auth />;
};

export default {
  title: "ProtoOS/Sign Up",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
