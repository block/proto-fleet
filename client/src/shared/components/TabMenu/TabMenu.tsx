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
  activePathAliases?: Partial<Record<string, string[]>>;
};

// Mark the TabMenu component with memo to prevent unnecessary re-renders
const TabMenu = memo(({ items, activePathAliases, basePath = "" }: TabMenuProps) => {
  const navigate = useNavigate();
  const location = useLocation();

  // Derive active item from location
  const activeItem = useMemo(() => {
    return Object.keys(items).find((key) => {
      const paths = [basePath + items[key].path, ...(activePathAliases?.[key] ?? [])];
      return paths.includes(location.pathname);
    });
  }, [location.pathname, items, activePathAliases, basePath]);

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
      className={clsx(
        "relative box-border grid w-full grid-cols-2 gap-2 p-2",
        "tablet:grid-cols-4 tablet:gap-1 tablet:p-0",
      )}
    >
      {tabElements}
    </div>
  );
});

TabMenu.displayName = "TabMenu";

export default TabMenu;
