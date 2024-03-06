import { ReactNode, useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import { navigationItems } from "../constants";

interface NavigationItemProps {
  icon: ReactNode;
  id: string;
  selected: keyof typeof navigationItems;
  setSelected: (selected: keyof typeof navigationItems) => void;
  text: string;
}

const NavigationItem = ({
  icon,
  id,
  selected,
  setSelected,
  text,
}: NavigationItemProps) => {
  const isSelected = useMemo(() => {
    return selected === id;
  }, [id, selected]);

  const handleClick = useCallback(() => {
    setSelected(id as keyof typeof navigationItems);
  }, [id, setSelected]);

  return (
    <Link
      className={clsx(
        "flex text-emphasis-300 items-center px-3 py-2 mb-3 rounded-md hover:cursor-pointer",
        {
          "text-text-emphasis bg-core-accent-fill/20 hover:bg-core-accent-fill/50":
            isSelected,
          "text-text-contrast/70 hover:bg-text-contrast/10": !isSelected,
        }
      )}
      onClick={handleClick}
      to={`/${id}`}
    >
      {icon}
      <span className="flex-grow ml-2">{text}</span>
    </Link>
  );
};

export default NavigationItem;
