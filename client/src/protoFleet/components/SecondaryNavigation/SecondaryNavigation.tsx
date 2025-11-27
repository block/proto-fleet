import { Link, useLocation } from "react-router-dom";
import { clsx } from "clsx";

import { type SecondaryNavItem } from "@/protoFleet/config/navItems";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type SecondaryNavigationProps = {
  items: SecondaryNavItem[];
};

const SecondaryNavigation = ({ items }: SecondaryNavigationProps) => {
  const { pathname } = useLocation();

  // Filter items to only show those whose parent matches the current path
  const visibleItems = items.filter((item) => {
    const _pathname = stripLeadingSlash(pathname);
    const _parent = stripLeadingSlash(item.parent);
    return _pathname === _parent || _pathname.startsWith(`${_parent}/`);
  });

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  // if current route has no secondary nav items
  // dont render anything
  if (visibleItems.length === 0) return null;

  return (
    <ul
      data-testid="secondary-nav"
      className="flex min-h-[calc(100vh-theme(spacing.1)*15)] w-[176px] flex-col gap-3 border-r border-border-5 px-2 pt-3 text-text-primary-70"
    >
      {visibleItems.map((item, idx) => {
        return (
          <li key={idx}>
            <Link
              to={"/" + stripLeadingSlash(item.path)}
              className={clsx(
                "block rounded-lg px-2 py-1 text-emphasis-300",
                "hover:text-text-primary",
                isCurrentPath(item.path) ? "bg-core-primary-5 text-text-primary" : "text-text-primary-70",
              )}
            >
              {item.label}
            </Link>
          </li>
        );
      })}
    </ul>
  );
};

export default SecondaryNavigation;
