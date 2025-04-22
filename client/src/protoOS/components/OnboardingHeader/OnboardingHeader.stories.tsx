import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import OnboardingHeader from ".";

export const Header = () => {
  return <OnboardingHeader />;
};

export default {
  title: "Components (Shared)/Onboarding/Header",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
