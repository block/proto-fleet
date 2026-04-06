import { Link, useLocation } from "react-router-dom";
import { clsx } from "clsx";

import { type SecondaryNavItem } from "@/protoFleet/config/navItems";
import { useRole } from "@/protoFleet/store";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type SecondaryNavigationProps = {
  items: SecondaryNavItem[];
};

const SecondaryNavigation = ({ items }: SecondaryNavigationProps) => {
  const { pathname } = useLocation();
  const { isPhone, isTablet } = useWindowDimensions();
  const currentRole = useRole();

  // Hide on mobile and tablet since secondary nav items are shown in main menu
  if (isPhone || isTablet) return null;

  // Filter items to only show those whose parent matches the current path and whose role matches
  const visibleItems = items.filter((item) => {
    const _pathname = stripLeadingSlash(pathname);
    const _parent = stripLeadingSlash(item.parent);
    const pathMatch = _pathname === _parent || _pathname.startsWith(`${_parent}/`);
    const roleMatch = !item.allowedRoles || item.allowedRoles.includes(currentRole);
    return pathMatch && roleMatch;
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
    <nav aria-label="Settings">
      <ul
        data-testid="secondary-nav"
        className="flex min-h-[calc(100vh-(--spacing(1))*15)] w-[176px] shrink-0 flex-col gap-3 px-3 pt-3 text-text-primary-70"
      >
        {visibleItems.map((item) => {
          return (
            <li key={item.path}>
              <Link
                to={"/" + stripLeadingSlash(item.path)}
                aria-current={isCurrentPath(item.path) ? "page" : undefined}
                className={clsx("block rounded-lg px-2 py-1 text-emphasis-300 text-text-primary-70", {
                  "bg-core-primary-5": isCurrentPath(item.path),
                })}
              >
                {item.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
};

export default SecondaryNavigation;
