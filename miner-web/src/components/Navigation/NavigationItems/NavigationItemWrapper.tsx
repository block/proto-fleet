import { useState } from "react";
import { useLocation } from "react-router-dom";

import { navigationItems } from "../constants";

import NavigationItem from "./NavigationItem";

const NavigationItemWrapper = () => {
  const location = useLocation();
  const { pathname } = location;
  const pageName = pathname.split("/")[1] as keyof typeof navigationItems;

  const [selected, setSelected] = useState(
    (navigationItems[pageName] ||
      navigationItems.performance) as keyof typeof navigationItems
  );

  return (
    <>
      <NavigationItem
        id={navigationItems.performance}
        text="Performance"
        selected={selected}
        setSelected={setSelected}
      />
      <NavigationItem
        id={navigationItems.hardware}
        text="Hardware"
        selected={selected}
        setSelected={setSelected}
      />
      <NavigationItem
        id={navigationItems.settings}
        text="Settings"
        selected={selected}
        setSelected={setSelected}
      />
      <NavigationItem
        id={navigationItems.help}
        text="Help"
        selected={selected}
        setSelected={setSelected}
      />
    </>
  );
};

export default NavigationItemWrapper;
