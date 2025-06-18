import { memo, useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";
import ActiveIndicator from "./ActiveIndicator/ActiveIndicator";
import Tab from "./Tab/Tab";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type TabMenuItem = {
  name: string;
  value?: number;
  units: string;
  path: string;
};

type TabMenuProps = {
  items: {
    [key: string]: TabMenuItem;
  };
  basePath?: string; // Optional base path for navigation
};

// Mark the TabMenu component with memo to prevent unnecessary re-renders
const TabMenu = memo(({ items, basePath = "" }: TabMenuProps) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [activeItem, setActiveItem] = useState<string>();
  const [activeIndex, setActiveIndex] = useState<number>();
  const [activeIndicatorTransX, setActiveIndicatorTransX] =
    useState<string>("0");
  const [activeIndicatorTransY, setActiveIndicatorTransY] =
    useState<string>("0");
  const { isPhone } = useWindowDimensions();
  const prevIsPhone = useRef(isPhone);
  // Start with animation disabled on initial render
  const [shouldAnimate, setShouldAnimate] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Turn animations on after the component has mounted
  useEffect(() => {
    // Enable animations after a delay to ensure initial position is set
    // 300ms seems to work reliably across different browsers and devices
    setTimeout(() => {
      setShouldAnimate(true);
    }, 300);
  }, []);

  useEffect(() => {
    const activeKey = Object.keys(items).find(
      (key) => basePath + items[key].path === location.pathname,
    );

    const idx = activeKey ? Object.keys(items).indexOf(activeKey) : undefined;
    setActiveIndex(idx);
    setActiveItem(activeKey);
  }, [location.pathname, items, basePath]);

  useEffect(() => {
    if (activeIndex === undefined) {
      return;
    }

    // if the user resizes the window we dont want indicator to animate
    if (isPhone !== prevIsPhone.current) {
      prevIsPhone.current = isPhone;
      setShouldAnimate(false);
      setTimeout(() => {
        setShouldAnimate(true);
      }, 300);
    }

    const transX = isPhone
      ? `calc(${(activeIndex % 2) * 100}% + 2 * var(--spacing) * ${activeIndex % 2})`
      : `calc(${activeIndex * 100}% + 2 * var(--spacing) * ${activeIndex})`;

    const transY = isPhone
      ? `calc(${Math.floor(activeIndex / 2) * 100}% + 2 * var(--spacing) * ${Math.floor(activeIndex / 2)})`
      : "0";

    setActiveIndicatorTransX(transX);
    setActiveIndicatorTransY(transY);
  }, [activeIndex, isPhone]);

  // Create memoized tab elements to prevent them from re-rendering
  const tabElements = Object.entries(items).map(
    ([key, { name, value, units, path }]) => (
      <Tab
        key={key}
        id={key}
        label={name}
        value={value}
        units={units}
        path={path}
        isActive={activeItem === key}
        onClick={() => {
          navigate(basePath + items[key].path);
        }}
      />
    ),
  );

  return (
    <div
      ref={containerRef}
      className={clsx(
        "relative box-border grid w-full grid-cols-4 gap-10",
        "phone:grid-cols-2 phone:gap-2 phone:p-2",

        // adds grey background behind tab nav that extends past its container width & height
        "before:absolute before:-top-[calc(theme(spacing.2))] before:-left-[calc(theme(spacing.6))] before:h-[calc(100%+theme(spacing.4))] before:w-[calc(100%+theme(spacing.12))] before:rounded-3xl before:bg-core-primary-5",
        "phone:before-h-full phone:before:top-0 phone:before:left-0 phone:before:h-full phone:before:w-full",
      )}
    >
      {/* Separate memo component for the active indicator */}
      <ActiveIndicator
        activeIndex={activeIndex}
        activeIndicatorTransX={activeIndicatorTransX}
        activeIndicatorTransY={activeIndicatorTransY}
        shouldAnimate={shouldAnimate}
      />

      {tabElements}
    </div>
  );
});

TabMenu.displayName = "TabMenu";

export default TabMenu;
