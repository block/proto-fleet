import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import OnboardingComponent from "./Onboarding";

export const Onboarding = () => {
  return <OnboardingComponent />;
};

export default {
  title: "ProtoOS/Onboarding/Mining Pools",
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
