import { ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { NavigationItemValue } from "../types";

interface NavigationItemProps {
  id?: string;
  isChildItem?: boolean;
  onClick: (selected: NavigationItemValue) => void;
  onHover?: (hover: boolean) => void;
  pageName?: string;
  suffixIcon?: ReactNode;
  text: string;
}

const NavigationItem = ({ id, isChildItem, onClick, onHover, pageName, suffixIcon, text }: NavigationItemProps) => {
  const isSelected = useMemo(() => {
    return pageName && pageName === id;
  }, [id, pageName]);

  const handleClick = useCallback(() => {
    onClick(id as NavigationItemValue);
  }, [id, onClick]);

  return (
    <button
      className={clsx("mb-3 flex w-full items-center rounded-lg py-1 text-left text-emphasis-300", {
        "bg-core-primary-5 text-text-primary": isSelected,
        "text-text-primary-70 hover:bg-core-primary-5": !isSelected,
        "px-6": isChildItem,
        "px-2": !isChildItem,
      })}
      onClick={handleClick}
      onMouseOver={() => onHover?.(true)}
      onMouseOut={() => onHover?.(false)}
    >
      <span className="grow">{text}</span>
      {suffixIcon}
    </button>
  );
};

export default NavigationItem;
