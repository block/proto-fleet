import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import Tab from "./Tab";

import "./style.css";

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
  const [slidingTab, setSlidingTab] = useState(initialTab);

  const handleSelectTab = (tab: string) => {
    setSlidingTab(tab);
  };

  const selectedTabIndex = childrenArray.indexOf(
    childrenArray?.find(
      (child) => child?.props?.label === activeTab
    ) as TabProps
  );

  const slidingTabIndex = childrenArray.indexOf(
    childrenArray?.find(
      (child) => child?.props?.label === slidingTab
    ) as TabProps
  );

  const distance = Math.abs(slidingTabIndex - selectedTabIndex);

  useEffect(() => {
    if (activeTab !== slidingTab) {
      setTimeout(() => {
        setActiveTab(slidingTab);
      }, 150);
    }
  }, [slidingTab, activeTab]);

  const tabs = childrenArray?.map((child: TabProps) => (
    <div className="text-text-primary/70" key={child.props.label}>
      <button
        onMouseDown={(e) => {
          e.preventDefault();
          handleSelectTab(child.props.label);
        }}
        className={clsx("relative pb-2 text-300", {
          "text-text-emphasis": child.props.label === slidingTab,
        })}
      >
        <div
          className={clsx("absolute w-full h-full", {
            "border-b-2 border-text-emphasis bottom-[-0.1rem]":
              child.props.label === activeTab,
            [`animate-tab-slide-right${distance}`]:
            selectedTabIndex < slidingTabIndex,
            [`animate-tab-slide-left${distance}`]:
            selectedTabIndex > slidingTabIndex,
          })}
        />
        <div className="relative">{child.props.label}</div>
      </button>
    </div>
  ));

  const tabContent = childrenArray?.map((tabContent) => (
    <div
      className={clsx("mt-6", {
        hidden: tabContent.props.label !== activeTab,
      })}
      key={`${tabContent.props.label}-content`}
    >
      {tabContent as ReactNode}
    </div>
  ));

  return (
    <>
      <div className="flex space-x-10 phone:space-x-6 text-emphasis-400 border-b-2 border-border-primary/5 whitespace-nowrap">
        {tabs}
      </div>
      {tabContent}
    </>
  );
};

Tabs.Tab = Tab;

export default Tabs;
