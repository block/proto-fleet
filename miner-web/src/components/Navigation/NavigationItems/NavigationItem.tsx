import { useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import Badge from "../badge";
import { navigationItems } from "../constants";

interface NavigationItemProps {
  id: string;
  selected: keyof typeof navigationItems;
  setSelected: (selected: keyof typeof navigationItems) => void;
  text: string;
}

const NavigationItem = ({
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
        "flex text-emphasis-400 text-text-primary/70 items-center h-10 px-3 py-2 mb-8 border-box rounded-md hover:cursor-pointer hover:bg-core-accent-fill/5",
        {
          "border border-text-emphasis/10 bg-core-accent-fill/5 text-text-emphasis ml-[-1px]":
            isSelected,
        }
      )}
      onClick={handleClick}
      to={`/${id}`}
    >
      <span className="flex-grow">{text}</span>
      {isSelected && <Badge status="warning" />}
    </Link>
  );
};

export default NavigationItem;
