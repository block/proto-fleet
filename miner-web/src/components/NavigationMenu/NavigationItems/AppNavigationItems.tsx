import { useCallback, useState } from "react";
import { AnimatePresence, motion } from "motion/react";

import { navigationItems } from "../constants";
import MorphingPlusMinus from "../MorphingPlusMinus";
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

  const handleClick = useCallback(
    (navigationItem: NavigationItemValue) => {
      onClick(navigationItem);
      setShowAccordionItems(navigationItem.startsWith("settings"));
    },
    [onClick]
  );

  return (
    <>
      <NavigationItem
        id={navigationItems.home}
        text="Home"
        onClick={handleClick}
        pageName={pageName}
      />
      <NavigationItem
        id={navigationItems.temperature}
        text="Temperature"
        onClick={handleClick}
        pageName={pageName}
      />
      <NavigationItem
        id={navigationItems.logs}
        text="Logs"
        onClick={handleClick}
        pageName={pageName}
      />
      <NavigationItem
        suffixIcon={
          showAccordionExpand || showAccordionItems ? (
            <MorphingPlusMinus condition={showAccordionExpand && !showAccordionItems} />
          ) : undefined
        }
        text="Settings"
        onClick={handleAccordionClick}
        onHover={handleAccordionHover}
      />
      <AnimatePresence>
        {showAccordionItems && (
          <motion.div 
            key="mining-pools"
            initial={{ opacity: 0, y: -12 }} 
            animate={{ opacity: 1, y: 0, transition: { duration: 0.4 }}} 
            exit={{ opacity: 0, y: -12, transition: { duration: 0.25 } }} 
          >
            <NavigationItem
              id={navigationItems.miningPools}
              text="Mining Pools"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
};

export default AppNavigationItems;
