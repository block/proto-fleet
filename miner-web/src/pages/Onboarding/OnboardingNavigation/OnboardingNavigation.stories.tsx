import { action } from "@storybook/addon-actions";

import OnboardingNavigationComponent from ".";

export const OnboardingNavigation = () => {
  return (
    <OnboardingNavigationComponent
      isVisible
      activeTab="pools"
      onChangeActiveTab={action("onChangeActiveTab")}
    />
  );
};

export default {
  title: "Pages/Onboarding/Onboarding Navigation",
};
