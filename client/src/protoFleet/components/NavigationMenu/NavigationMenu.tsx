import { createElement, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { NavRoute } from "@/protoFleet/routes";
import { LogoAlt } from "@/shared/assets/icons";
import { pick } from "@/shared/utils/object";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type NavigationMenuProps = {
  routes: NavRoute[];
};

const NavigationMenu = ({ routes }: NavigationMenuProps) => {
  const { pathname } = useLocation();

  const navigationItems = useMemo(
    () =>
      routes
        .filter((route) => route.navItem)
        .map((route) => pick(route, ["label", "path", "icon"])),
    [routes],
  );

  const homeItem = useMemo(
    () => navigationItems.find((item) => item.label === "Home"),
    [navigationItems],
  );

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  return (
    <div
      className={clsx(
        "flex min-h-screen w-[64px] flex-col border-r border-border-5 bg-grayscale-gray-5 text-text-primary-70",
        // "tablet:absolute tablet:z-30 tablet:min-h-[calc(100vh-16px)]",
        // "phone:absolute phone:z-30 phone:min-h-[calc(100vh-16px)]",
      )}
    >
      <div className="flex flex-col items-center justify-center gap-[10px]">
        {homeItem && homeItem.path && (
          <div className="flex h-[60px] items-center justify-center px-3 py-2">
            <Link to={homeItem.path}>
              <LogoAlt className="text-text-primary-30 hover:cursor-pointer" />
            </Link>
          </div>
        )}

        <ul
          data-testid="navigation-menu"
          className="flex flex-col items-center justify-center gap-[10px]"
        >
          {navigationItems.map((item, idx) => {
            return item.path ? (
              <li key={idx}>
                <Link
                  to={item.path}
                  className={clsx(
                    "group flex h-[40px] w-[40px] items-center justify-center rounded-lg",
                    "hover:cursor-pointer hover:bg-core-primary-10",
                    isCurrentPath(item.path)
                      ? "bg-core-primary-10 text-text-primary"
                      : "text-text-primary-50",
                  )}
                >
                  {item.icon
                    ? createElement(item.icon, {
                        className:
                          "transition-transform duration-200 ease-gentle group-hover:scale-105",
                      })
                    : item.label}
                </Link>
              </li>
            ) : null;
          })}
        </ul>
      </div>
    </div>
  );
};

export default NavigationMenu;
