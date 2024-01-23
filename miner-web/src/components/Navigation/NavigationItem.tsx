import clsx from "clsx";
import { Link } from "react-router-dom";
import { navigationItems } from "./constants";
import { useCallback, useMemo } from "react";

interface NavigationItemProps {
  icon: string;
  id: string;
  selected: keyof typeof navigationItems;
  setSelected: (selected: keyof typeof navigationItems) => void;
  text: string;
}

const NavigationItem = ({ icon, id, selected, setSelected, text }: NavigationItemProps) => {
  const isSelected = useMemo(() => {
    return selected === id
  }, [id, selected]);

  const handleClick = useCallback(() => {
    setSelected(id as keyof typeof navigationItems);
  }, [id, setSelected]);

  return (
    <Link
      className={clsx(
        "flex text-body-default items-center h-9 px-2 py-4 mb-8 hover:cursor-pointer border-box navigation-item hover:rounded-lg",
        { selected: isSelected, "border-[1px] border-primary-100 rounded-lg ml-[-1px]": isSelected }
      )}
      onClick={handleClick}
      to={`/${id}`}
    >
      <img className="mr-2" src={icon} alt="Dashboard" />
      {text}
    </Link>
  );
};

export default NavigationItem;
