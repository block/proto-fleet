import { AnimatePresence, motion } from "motion/react";
import { useCallback, useState } from "react";

import { navigationItems } from "../constants";
import NavigationItem from "../NavigationItem";
import { NavigationItemValue } from "../types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDeviceType } from "@/protoOS/store";
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
  const { isFleetHosted } = useMinerHosting();
  const deviceType = useDeviceType();
  const homeNavigationItem = deviceType === "container" ? navigationItems.containerHome : navigationItems.home;

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
      <NavigationItem id={homeNavigationItem} text="Home" onClick={handleClick} pageName={pageName} />
      <NavigationItem id={navigationItems.diagnostics} text="Diagnostics" onClick={handleClick} pageName={pageName} />
      {deviceType === "container" ? (
        <>
          <NavigationItem id={navigationItems.tanks} text="Tanks" onClick={handleClick} pageName={pageName} />
          <NavigationItem id={navigationItems.modules} text="Modules" onClick={handleClick} pageName={pageName} />
          <NavigationItem id={navigationItems.pools} text="Pools" onClick={handleClick} pageName={pageName} />
        </>
      ) : null}
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
            {/* Authentication (password change) is Fleet-managed in the
                embedded view — the proxy blocks that write and it has no useful
                read-only view — so hide it. Pools stays visible: pool edits are
                also Fleet-managed, but the page shows the miner's current pools
                read-only so operators can see what's configured. */}
            {isFleetHosted || deviceType === "container" ? null : (
              <NavigationItem
                id={navigationItems.authentication}
                text="Authentication"
                onClick={handleClick}
                pageName={pageName}
                isChildItem
              />
            )}
            <NavigationItem
              id={navigationItems.general}
              text="General"
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={deviceType === "container" ? navigationItems.hardware : navigationItems.miningPools}
              text={deviceType === "container" ? "Hardware" : "Pools"}
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            <NavigationItem
              id={deviceType === "container" ? navigationItems.miningPools : navigationItems.hardware}
              text={deviceType === "container" ? "Pools" : "Hardware"}
              onClick={handleClick}
              pageName={pageName}
              isChildItem
            />
            {deviceType !== "container" ? (
              <NavigationItem
                id={navigationItems.cooling}
                text="Cooling"
                onClick={handleClick}
                pageName={pageName}
                isChildItem
              />
            ) : null}
          </motion.div>
        ) : null}
      </AnimatePresence>
    </>
  );
};

export default AppNavigationItems;
