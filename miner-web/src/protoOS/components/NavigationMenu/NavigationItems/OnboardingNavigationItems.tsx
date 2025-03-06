import { navigationItems } from "../constants";
import NavigationItem from "../NavigationItem";
import { NavigationItemValue } from "../types";

interface OnboardingNavigationItemsProps {
  onClick: (navigationItem: NavigationItemValue) => void;
  pageName: string;
}

const OnboardingNavigationItems = ({
  onClick,
  pageName,
}: OnboardingNavigationItemsProps) => {
  return (
    <>
      <div className="text-heading-100 text-text-primary mb-1">Miner setup</div>
      <div className="text-200 text-text-primary-70 mb-3">
        Complete the steps below to set up your miner.
      </div>
      <NavigationItem
        id={navigationItems.onboarding}
        text="Mining Pools"
        onClick={onClick}
        pageName={pageName}
      />
    </>
  );
};

export default OnboardingNavigationItems;
