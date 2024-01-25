import { useCallback, useMemo } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import Badge from "./badge";
import { navigationItems } from "./constants";

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
        "flex text-body-default text-foreground-60 items-center h-10 px-3 py-2 mb-8 border-box rounded-md hover:cursor-pointer hover:bg-warning-100/5",
        {
          "border-2 border-warning-100/10 bg-warning-100/5 text-warning-100 ml-[-2px]":
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
