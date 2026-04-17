import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import OnboardingHeader from ".";

export const Header = () => {
  return <OnboardingHeader />;
};

export default {
  title: "Shared/Onboarding/Header",
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
