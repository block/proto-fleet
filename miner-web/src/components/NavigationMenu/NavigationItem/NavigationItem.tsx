import { ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { NavigationItemValue } from "../types";

interface NavigationItemProps {
  icon?: ReactNode;
  id?: string;
  onClick: (selected: NavigationItemValue) => void;
  onHover?: (hover: boolean) => void;
  pageName?: string;
  suffixIcon?: ReactNode;
  text: string;
}

const NavigationItem = ({ icon, id, onClick, onHover, pageName, suffixIcon, text }: NavigationItemProps) => {
  const isSelected = useMemo(() => {
    return pageName && pageName === id;
  }, [id, pageName]);

  const handleClick = useCallback(() => {
    onClick(id as NavigationItemValue);
  }, [id, onClick]);

  return (
    <button
      className={clsx(
        "flex text-emphasis-300 items-center px-3 py-2 mb-3 rounded-md w-full text-left",
        {
          "text-text-emphasis bg-core-accent-fill/20 hover:bg-core-accent-fill/50":
            isSelected,
          "text-text-contrast/70 hover:bg-text-contrast/10": !isSelected,
        }
      )}
      onClick={handleClick}
      onMouseOver={() => onHover?.(true)}
      onMouseOut={() => onHover?.(false)}
    >
      {icon}
      <span className={clsx("flex-grow", { "ml-2": icon, "ml-7": !icon })}>
        {text}
      </span>
      {suffixIcon}
    </button>
  );
};

export default NavigationItem;
