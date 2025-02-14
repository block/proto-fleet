import { Link } from "react-router-dom";
import { clsx } from "clsx";

type Props = {
  currentPage?: boolean;
  items: {
    name: string;
    route: string;
  }[];
};

const SecondaryNavigation = ({ items, currentPage = false }: Props) => {
  return (
    <div className="flex flex-col gap-2 text-text-primary-70 w-[176px] border-r border-border-5 px-2 pt-4">
      {items.map((item, idx) => (
        <Link
          key={idx}
          to={item.route}
          className={clsx(
            currentPage ? "bg-black" : "",
            "px-2 rounded-md hover:text-text-primary"
          )}
        >
          {item.name}
        </Link>
      ))}
    </div>
  );
};

export default SecondaryNavigation;
