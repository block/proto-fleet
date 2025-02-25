import { createElement, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";

import { NavRoute } from "@/protoFleet/routes";
import { LogoAlt } from "@/shared/assets/icons";
import { pick } from "@/shared/utils/object";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type NavigationMenuProps = {
  routes: NavRoute[];
}

const NavigationMenu = ({ routes }: NavigationMenuProps) => {
  const { pathname } = useLocation();

  const navigationItems = useMemo(
    () => routes
      .filter((route) => route.navItem)
      .map((route) => pick(route, ["label", "path", "icon"])),
    [routes]
  );

  const homeItem = useMemo(
    () => navigationItems.find((item) => item.label === "Home"), 
    [navigationItems]
  );

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`)
  };
  
  return (
    <div
      className={clsx(
        "w-[64px] min-h-screen flex flex-col bg-grayscale-gray-5 text-text-primary-70 border-r border-border-5",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute"
      )}
    >
      <div className="flex items-center justify-center flex-col gap-[10px]">
        {homeItem && homeItem.path && (
          <div className="h-[60px] px-3 py-2 flex items-center justify-center">
            <Link to={homeItem.path}>
              <LogoAlt className="hover:cursor-pointer text-text-primary-30" />
            </Link>
          </div>
        )}

        <ul data-testid="navigation-menu" className="flex items-center justify-center flex-col gap-[10px]">
          {navigationItems.map((item, idx) => {
            return item.path ? (
              <li key={idx}>
                <Link
                  to={item.path}
                  className={clsx(
                    "group flex items-center justify-center w-[40px] h-[40px] rounded-lg",
                    "hover:cursor-pointer hover:bg-core-primary-10",
                    isCurrentPath(item.path)
                      ? "bg-core-primary-10 text-text-primary"
                      : "text-text-primary-50"
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
