import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import Tab from "./Tab/Tab";

import { useMinerHosting } from "@/protoOS/api";

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
};

const TabMenu = ({ items }: TabMenuProps) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [activeItem, setActiveItem] = useState<string>();
  const { minerRoot } = useMinerHosting();

  useEffect(() => {
    const activeKey = Object.keys(items).find(
      (key) => minerRoot + items[key].path === location.pathname,
    );
    setActiveItem(activeKey);
  }, [location.pathname, items, minerRoot]);

  return (
    <div className="flex w-full flex-wrap items-center gap-2 space-x-4 rounded-3xl bg-core-primary-5 p-2">
      {Object.entries(items).map(([key, { name, value, units, path }], idx) => {
        return (
          <Tab
            key={idx}
            id={key}
            label={name}
            value={value}
            units={units}
            path={path}
            isActive={activeItem === key}
            onClick={() => {
              navigate(minerRoot + items[key].path);
            }}
          />
        );
      })}
    </div>
  );
};

export default TabMenu;
