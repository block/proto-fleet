import { AnimatePresence, motion } from "motion/react";
import { useCallback, useState } from "react";

import { navigationItems } from "../constants";
import NavigationItem from "../NavigationItem";
import { NavigationItemValue } from "../types";
import MorphingPlusMinus from "@/shared/components/MorphingPlusMinus";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";

interface AppNavigationItemsProps {
  onClick: (navigationItem: NavigationItemValue) => void;
  pageName: string;
}

const AppNavigationItems = ({ onClick, pageName }: AppNavigationItemsProps) => {
  const [showAccordionItems, setShowAccordionItems] = useState(pageName.startsWith("settings"));
  const [showAccordionExpand, setShowAccordionExpand] = useState(false);

  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

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
    [onClick],
  );

  return (
    <>
      <NavigationItem id={navigationItems.home} text="Home" onClick={handleClick} pageName={pageName} />
      <NavigationItem id={navigationItems.diagnostics} text="Diagnostics" onClick={handleClick} pageName={pageName} />
      <NavigationItem id={navigationItems.logs} text="Logs" onClick={handleClick} pageName={pageName} />
      <NavigationItem
        suffixIcon={
          showAccordionExpand || showAccordionItems ? (
            <MorphingPlusMinus condition={showAccordionExpand ? !showAccordionItems : false} />
          ) : undefined
        }
        text="Settings"
        onClick={handleAccordionClick}
        onHover={handleAccordionHover}
      />
      <AnimatePresence>
        {showAccordionItems ? (
          <motion.div
            initial={{ opacity: 0, y: -12 }}
            animate={{
              opacity: 1,
              y: 0,
              transition: { duration: 0.3, ease: easeGentle },
            }}
            exit={{
              opacity: 0,
              y: -12,
              transition: { duration: 0.3, ease: easeGentle },
            }}
          >
            <NavigationItem
              id={navigationItems.authentication}
              text="Authentication"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={navigationItems.general}
              text="General"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={navigationItems.miningPools}
              text="Pools"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={navigationItems.hardware}
              text="Hardware"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={navigationItems.cooling}
              text="Cooling"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
          </motion.div>
        ) : null}
      </AnimatePresence>
    </>
  );
};

export default AppNavigationItems;
