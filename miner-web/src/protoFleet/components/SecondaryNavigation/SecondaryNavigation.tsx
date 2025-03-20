import { Link, matchPath, useLocation } from "react-router-dom";
import { clsx } from "clsx";

import { type NavRoute } from "@/protoFleet/routes";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

const getSecondarybNavItems = (routes: NavRoute[], pathname: string) => {
  const currentRoute = routes.find((r) => {
    if (!r.path) return false;
    return matchPath(r.path, pathname);
  });

  const secondaryNavItems = routes.filter(
    (route) =>
      currentRoute?.secondaryNavItem &&
      route.secondaryNavItem == currentRoute.secondaryNavItem,
  );

  return secondaryNavItems;
};

type SecondaryNavigationProps = {
  routes: NavRoute[];
};

const SecondaryNavigation = ({ routes }: SecondaryNavigationProps) => {
  const { pathname } = useLocation();
  const items = getSecondarybNavItems(routes, pathname);

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  // if current route has no secondary nav items
  // dont render anything
  if (items.length === 0) return null;

  return (
    <ul
      data-testid="secondary-nav"
      className="flex w-[176px] flex-col gap-3 border-r border-border-5 px-2 pt-3 text-text-primary-70"
    >
      {items.map((item, idx) => {
        if (!item.path) return;

        return (
          <li key={idx}>
            <Link
              to={item.path}
              className={clsx(
                "block rounded-lg px-2 py-1 text-emphasis-300",
                "hover:text-text-primary",
                isCurrentPath(item.path)
                  ? "bg-core-primary-5 text-text-primary"
                  : "text-text-primary-70",
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
