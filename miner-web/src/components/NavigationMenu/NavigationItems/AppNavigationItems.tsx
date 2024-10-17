import { useCallback, useState } from "react";

import { Minus, Plus } from "icons";

import { navigationItems } from "../constants";
import NavigationItem from "../NavigationItem";
import { NavigationItemValue } from "../types";

interface AppNavigationItemsProps {
  onClick: (navigationItem: NavigationItemValue) => void;
  pageName: string;
}

const AppNavigationItems = ({ onClick, pageName }: AppNavigationItemsProps) => {
  const [showAccordionItems, setShowAccordionItems] = useState(
    pageName.startsWith("settings")
  );
  const [showAccordionExpand, setShowAccordionExpand] = useState(false);

  const handleAccordionClick = useCallback(() => {
    setShowAccordionItems((prev) => !prev);
  }, []);

  const handleAccordionHover = useCallback((hover: boolean) => {
    setShowAccordionExpand(hover);
  }, []);

  return (
    <>
      <NavigationItem
        id={navigationItems.home}
        text="Home"
        onClick={onClick}
        pageName={pageName}
      />
      <NavigationItem
        id={navigationItems.temperature}
        text="Temperature"
        onClick={onClick}
        pageName={pageName}
      />
      <NavigationItem
        id={navigationItems.logs}
        text="Logs"
        onClick={onClick}
        pageName={pageName}
      />
      <NavigationItem
        suffixIcon={
          showAccordionExpand || showAccordionItems ? (
            showAccordionExpand && !showAccordionItems ? (
              <Plus />
            ) : (
              <Minus />
            )
          ) : undefined
        }
        text="Settings"
        onClick={handleAccordionClick}
        onHover={handleAccordionHover}
      />
      {showAccordionItems && (
        <>
          <NavigationItem
            id={navigationItems.miningPools}
            text="Mining Pools"
            onClick={onClick}
            pageName={pageName}
            isChildItem
          />
        </>
      )}
    </>
  );
};

export default AppNavigationItems;
