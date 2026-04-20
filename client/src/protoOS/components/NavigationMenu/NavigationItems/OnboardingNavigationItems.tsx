import { navigationItems } from "../constants";
import NavigationItem from "../NavigationItem";
import { NavigationItemValue } from "../types";

interface OnboardingNavigationItemsProps {
  onClick: (navigationItem: NavigationItemValue) => void;
  pageName: string;
}

const OnboardingNavigationItems = ({ onClick, pageName }: OnboardingNavigationItemsProps) => {
  return (
    <>
      <div className="mb-1 text-heading-100 text-text-primary">Miner setup</div>
      <div className="mb-3 text-200 text-text-primary-70">Complete the steps below to set up your miner.</div>
      <NavigationItem id={navigationItems.onboarding} text="Pools" onClick={onClick} pageName={pageName} />
    </>
  );
};

export default OnboardingNavigationItems;
