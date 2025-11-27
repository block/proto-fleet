import { memo, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";
import Tab from "./Tab/Tab";

type TabMenuItem = {
  name: string;
  value?: number | string;
  units?: string;
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

  // Derive active item from location
  const activeItem = useMemo(() => {
    return Object.keys(items).find((key) => basePath + items[key].path === location.pathname);
  }, [location.pathname, items, basePath]);

  // Create memoized tab elements to prevent them from re-rendering
  const tabElements = Object.entries(items).map(([key, { name, value, units, path }]) => (
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
  ));

  return (
    <div
      className={clsx("relative box-border grid w-full grid-cols-4 gap-1", "phone:grid-cols-2 phone:gap-2 phone:p-2")}
    >
      {tabElements}
    </div>
  );
});

TabMenu.displayName = "TabMenu";

export default TabMenu;
