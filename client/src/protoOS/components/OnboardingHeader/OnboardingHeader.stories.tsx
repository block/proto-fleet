import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import OnboardingHeader from ".";

export const Header = () => {
  return <OnboardingHeader />;
};

export default {
  title: "Proto OS/Onboarding/Header",
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
