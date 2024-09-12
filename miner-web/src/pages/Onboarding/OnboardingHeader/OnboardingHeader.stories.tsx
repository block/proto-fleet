import { action } from "@storybook/addon-actions";

import { variants } from "components/Button";

import OnboardingHeaderComponent from ".";

export const OnboardingHeader = () => {
  return (
    <OnboardingHeaderComponent
      openMenu={action("Open menu clicked")}
      button={{
        text: "Finish setup",
        onClick: action("Button clicked"),
        variant: variants.accent,
      }}
    />
  );
};

export default {
  title: "Pages/Onboarding/Onboarding Header",
};
