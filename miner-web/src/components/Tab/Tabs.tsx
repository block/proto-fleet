import { ReactNode, useState } from "react";
import clsx from "clsx";

import Tab from "./Tab";

interface TabProps {
  props: { label: string };
}

interface TabsProps {
  children: TabProps | TabProps[];
}

const Tabs = ({ children }: TabsProps) => {
  const childrenArray = Array.isArray(children) ? children : [children];
  const initialTab = childrenArray[0].props.label;
  const [activeTab, setActiveTab] = useState(initialTab);

  const tabs = childrenArray?.map((child: TabProps) => (
    <button
      onClick={(e) => {
        e.preventDefault();
        setActiveTab(child.props.label);
      }}
      className={clsx("pb-2", {
        "text-text-emphasis border-b-2 border-text-emphasis mb-[-0.1rem]":
          child.props.label === activeTab,
        "text-text-primary/70": child.props.label !== activeTab,
      })}
      key={child.props.label}
    >
      {child.props.label}
    </button>
  ));

  const tabContent = childrenArray?.filter(
    (child: TabProps) => child.props.label === activeTab
  );

  return (
    <>
      <div className="flex space-x-10 phone:space-x-6 text-emphasis-400 border-b-2 border-border-primary/5 whitespace-nowrap">
        {tabs}
      </div>
      <div className="mt-6">{tabContent as ReactNode}</div>
    </>
  );
};

Tabs.Tab = Tab;

export default Tabs;
