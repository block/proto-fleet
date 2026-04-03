import { AnimatePresence, motion } from "motion/react";
import { createElement, useCallback, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";
import { useLogoutAction } from "@/protoFleet/api/useLogout";
import { NavItem, secondaryNavItems } from "@/protoFleet/config/navItems";
import { useRole } from "@/protoFleet/store";
import { Logo, LogoAlt } from "@/shared/assets/icons";
import { ArrowLeftCompact } from "@/shared/assets/icons";
import MorphingPlusMinus from "@/shared/components/MorphingPlusMinus";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";
import { cubicBezierValues } from "@/shared/utils/cssUtils";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type NavigationProps = {
  items: NavItem[];
  className?: string;
  closeMenu?: () => void;
};

const Navigation = ({ items, className, closeMenu }: NavigationProps) => {
  const { pathname } = useLocation();
  const { isPhone, isTablet } = useWindowDimensions();
  const logout = useLogoutAction();
  const currentRole = useRole();
  const [settingsManuallyToggled, setSettingsManuallyToggled] = useState(false);
  const [showSettingsHover, setShowSettingsHover] = useState(false);

  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  const homeItem = useMemo(() => items.find((item) => item.label === "Home"), [items]);
  const settingsItem = useMemo(() => items.find((item) => item.label === "Settings"), [items]);

  // Check if current page is a settings sub-item
  const isOnSettingsSubPage = useMemo(() => {
    const _pathname = stripLeadingSlash(pathname);
    return secondaryNavItems
      .filter((nav) => nav.parent === "/settings")
      .some((nav) => {
        const _navPath = stripLeadingSlash(nav.path);
        return _pathname === _navPath || _pathname.startsWith(`${_navPath}/`);
      });
  }, [pathname]);

  // Derive expanded state: auto-expand if on settings page OR manually toggled
  const isSettingsExpanded = settingsManuallyToggled || isOnSettingsSubPage;

  const handleSettingsHover = useCallback((hover: boolean) => {
    setShowSettingsHover(hover);
  }, []);

  const isCurrentPath = (path: string) => {
    const _pathname = stripLeadingSlash(pathname);
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  return (
    <div
      className={clsx(
        "flex min-h-screen w-60 flex-col justify-between bg-surface-base text-text-primary-70 laptop:w-full laptop:bg-surface-5 laptop:dark:bg-surface-base desktop:w-full desktop:bg-surface-5 desktop:dark:bg-surface-base",
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

        <ul data-testid="navigation-menu" className="flex w-full flex-col items-center justify-center gap-3 px-3">
          {items.map((item, idx) => {
            // Skip Settings item on mobile/tablet if it has secondary nav items - we'll render it separately with expand/collapse
            if (
              (isPhone || isTablet) &&
              item.path === "/settings" &&
              secondaryNavItems.some((nav) => nav.parent === item.path)
            ) {
              return null;
            }

            return item.path ? (
              <li key={idx} className="w-full">
                <Link
                  to={item.path}
                  onClick={() => closeMenu?.()}
                  className={clsx(
                    "group flex items-center justify-start rounded-lg px-2 py-1 laptop:justify-center desktop:justify-center",
                    "hover:cursor-pointer hover:bg-core-primary-5",
                    isCurrentPath(item.path) || isPhone || isTablet ? "text-text-primary" : "text-text-primary-50",
                    {
                      "bg-core-primary-5": isCurrentPath(item.path),
                    },
                  )}
                >
                  {item.icon
                    ? createElement(item.icon, {
                        className: "transition-transform duration-200 ease-gentle group-hover:scale-105",
                        width: "w-5",
                      })
                    : item.label}
                  {(isPhone || isTablet) && item.icon && (
                    <span className="ml-2 text-emphasis-300 text-text-primary-70">{item.label}</span>
                  )}
                </Link>
              </li>
            ) : null;
          })}

          {/* On mobile/tablet: show expandable Settings menu */}
          {(isPhone || isTablet) &&
            settingsItem &&
            secondaryNavItems.filter((nav) => nav.parent === "/settings").length > 0 && (
              <>
                <li className="w-full">
                  <button
                    onClick={() => setSettingsManuallyToggled(!settingsManuallyToggled)}
                    onMouseEnter={() => handleSettingsHover(true)}
                    onMouseLeave={() => handleSettingsHover(false)}
                    aria-expanded={isSettingsExpanded}
                    aria-controls="settings-submenu"
                    aria-label="Settings menu toggle"
                    className={clsx(
                      "group flex w-full items-center justify-start rounded-lg px-2 py-1 text-text-primary",
                      "hover:cursor-pointer hover:bg-core-primary-5",
                    )}
                  >
                    {settingsItem.icon &&
                      createElement(settingsItem.icon, {
                        className: "transition-transform duration-200 ease-gentle group-hover:scale-105",
                        width: "w-5",
                      })}
                    <span className="ml-2 flex-1 text-left text-emphasis-300 text-text-primary-70">
                      {settingsItem.label}
                    </span>
                    {(showSettingsHover || isSettingsExpanded) && (
                      /*
                       * Show MorphingPlusMinus icon when either hovered or expanded.
                       * - When hovering and not expanded, show plus (indicates expandable).
                       * - When expanded, show minus (indicates collapsible).
                       */
                      <MorphingPlusMinus condition={showSettingsHover && !isSettingsExpanded} />
                    )}
                  </button>
                </li>

                {/* Show secondary nav items when expanded */}
                <AnimatePresence>
                  {isSettingsExpanded && (
                    <motion.div
                      id="settings-submenu"
                      data-testid="secondary-nav"
                      initial={{ opacity: 0, y: -12 }}
                      animate={{
                        opacity: 1,
                        y: 0,
                        transition: { duration: 0.3, ease: easeGentle },
                      }}
                      exit={{
                        opacity: 0,
                        y: -12,
                        transition: { duration: 0.3, ease: easeGentle },
                      }}
                      className="flex w-full flex-col gap-3"
                    >
                      {secondaryNavItems
                        .filter((nav) => nav.parent === "/settings")
                        .filter((nav) => !nav.allowedRoles || nav.allowedRoles.includes(currentRole))
                        .map((nav) => (
                          <li key={nav.path} className="w-full">
                            <Link
                              to={nav.path}
                              onClick={() => closeMenu?.()}
                              className={clsx(
                                "block rounded-lg px-9 py-1 text-emphasis-300 text-text-primary-70",
                                "hover:cursor-pointer hover:bg-core-primary-5",
                                {
                                  "bg-core-primary-5": isCurrentPath(nav.path),
                                },
                              )}
                            >
                              {nav.label}
                            </Link>
                          </li>
                        ))}
                    </motion.div>
                  )}
                </AnimatePresence>
              </>
            )}
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
          data-testid="logout-button"
        >
          <ArrowLeftCompact className="text-text-primary-50 transition-transform duration-200 ease-gentle group-hover:scale-105" />
          {(isPhone || isTablet) && <span className="ml-2 text-emphasis-300 text-text-primary-70">Logout</span>}
        </button>
      </div>
    </div>
  );
};

export default Navigation;
