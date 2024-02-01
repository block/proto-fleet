import { useCallback, useState } from "react";
import clsx from "clsx";

interface TabWrapperProps {
  children: any[];
}

const TabWrapper = ({ children }: TabWrapperProps) => {
  const initialTab: string = children[0].props.label;
  const [activeTab, setActiveTab] = useState(initialTab);
  const handleActiveTab = useCallback(
    (label: string) => setActiveTab(label),
    []
  );

  const tabs = children?.map((child) => (
    <button
      onClick={(e) => {
        e.preventDefault();
        handleActiveTab(child.props.label);
      }}
      className={clsx("pb-2", {
        "text-warning-100 border-b-2 border-warning-100 mb-[-0.1rem]":
          child.props.label === activeTab,
      })}
      key={child.props.label}
    >
      {child.props.label}
    </button>
  ));

  const tabContent = children?.filter(
    (child) => child.props.label === activeTab
  );

  return (
    <>
      <div className="flex space-x-10 text-body-default font-semibold leading-8 tracking-[-0.4px] border-b-2 border-foreground-100/10">
        {tabs}
      </div>
      <div className="mt-6">{tabContent}</div>
    </>
  );
};

export default TabWrapper;
