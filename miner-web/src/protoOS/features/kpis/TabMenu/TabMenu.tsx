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
    <div className="flex gap-2 items-center space-x-4 rounded-3xl bg-core-primary-5 p-2 w-full flex-wrap">
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
