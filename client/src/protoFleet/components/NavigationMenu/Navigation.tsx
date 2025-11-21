import { createElement, useMemo } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";
import { NavItem } from "@/protoFleet/config/navItems";
import { useLogout } from "@/protoFleet/store";
import { Logo, LogoAlt } from "@/shared/assets/icons";
import { ArrowLeftCompact } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type NavigationProps = {
  items: NavItem[];
  className?: string;
};

const Navigation = ({ items, className }: NavigationProps) => {
  const { pathname } = useLocation();
  const { isPhone, isTablet } = useWindowDimensions();
  const logout = useLogout();

  const homeItem = useMemo(
    () => items.find((item) => item.label === "Home"),
    [items],
  );

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  return (
    <div
      className={clsx(
        "flex min-h-screen w-60 flex-col justify-between bg-surface-base text-text-primary-70 laptop:w-full laptop:bg-surface-5 desktop:w-full desktop:bg-surface-5",
        "laptop:items-center desktop:items-center",
        "tablet:absolute tablet:z-30",
        "phone:absolute phone:z-30",
        className,
      )}
    >
      <div className="flex flex-col items-center justify-center gap-3">
        {homeItem && homeItem.path && (
          <div
            className={clsx(
              "flex h-15 w-full items-start justify-center px-3 py-3 laptop:h-13 laptop:items-center laptop:!pb-0 desktop:h-13 desktop:items-center desktop:!pb-0",
              {
                "border-b border-border-5": isPhone || isTablet,
              },
            )}
          >
            <Link
              to={homeItem.path}
              className={clsx({
                "w-full": isPhone || isTablet,
              })}
            >
              {isPhone || isTablet ? (
                <Logo className="h-10 text-text-primary hover:cursor-pointer" />
              ) : (
                <LogoAlt className="text-text-primary-30 hover:cursor-pointer" />
              )}
            </Link>
          </div>
        )}

        <ul
          data-testid="navigation-menu"
          className="flex w-full flex-col items-center justify-center gap-3 px-3"
        >
          {items.map((item, idx) => {
            return item.path ? (
              <li key={idx} className="w-full">
                <Link
                  to={item.path}
                  className={clsx(
                    "group flex items-center justify-start rounded-lg p-2.5 laptop:justify-center desktop:justify-center",
                    "hover:cursor-pointer hover:bg-core-primary-5",
                    isCurrentPath(item.path) || isPhone || isTablet
                      ? "text-text-primary"
                      : "text-text-primary-50",
                    {
                      "bg-core-primary-5": isCurrentPath(item.path),
                    },
                  )}
                >
                  {item.icon
                    ? createElement(item.icon, {
                        className:
                          "transition-transform duration-200 ease-gentle group-hover:scale-105",
                        width: "w-5",
                      })
                    : item.label}
                  {(isPhone || isTablet) && item.icon && (
                    <span className="ml-2 text-emphasis-300 text-text-primary-70">
                      {item.label}
                    </span>
                  )}
                </Link>
              </li>
            ) : null;
          })}
        </ul>
      </div>
      <div className="pb-3 phone:px-3 tablet:px-3">
        <button
          onClick={() => {
            logout();
          }}
          className={clsx(
            "group flex h-8 w-full items-center justify-start rounded-lg px-2 py-1 laptop:h-10 laptop:justify-center desktop:h-10 desktop:justify-center",
            "hover:cursor-pointer hover:bg-core-primary-10",
          )}
        >
          <ArrowLeftCompact className="text-text-primary-50 transition-transform duration-200 ease-gentle group-hover:scale-105" />
          {(isPhone || isTablet) && (
            <span className="ml-2 text-emphasis-300 text-text-primary-70">
              Logout
            </span>
          )}
        </button>
      </div>
    </div>
  );
};

export default Navigation;
