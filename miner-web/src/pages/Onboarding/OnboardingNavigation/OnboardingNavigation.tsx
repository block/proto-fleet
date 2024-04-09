import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { Tabs } from "../types";
import FloatingNavigation from "./FloatingNavigation";
import Navigation from "./Navigation";

interface OnboardingNavigationProps {
  activeTab: Tabs;
  closeMenu?: () => void;
  isVisible?: boolean;
  poolUrls?: string[];
  onChangeActiveTab: (tab: Tabs) => void;
}

const OnboardingNavigation = ({
  activeTab,
  closeMenu,
  isVisible,
  poolUrls = [],
  onChangeActiveTab,
}: OnboardingNavigationProps) => {
  const { isPhone, isTablet } = useWindowDimensions();

  if (isPhone || isTablet) {
    if (isVisible) {
      return (
        <FloatingNavigation
          activeTab={activeTab}
          closeMenu={closeMenu}
          poolUrls={poolUrls}
          onChangeActiveTab={onChangeActiveTab}
        />
      );
    }
    return null;
  }

  return (
    <Navigation
      activeTab={activeTab}
      poolUrls={poolUrls}
      onChangeActiveTab={onChangeActiveTab}
    />
  );
};

export default OnboardingNavigation;
