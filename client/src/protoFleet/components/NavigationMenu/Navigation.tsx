import { AnimatePresence, motion } from "motion/react";
import { createElement, useCallback, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";
import { useLogoutAction } from "@/protoFleet/api/useLogout";
import { NavItem, secondaryNavItems } from "@/protoFleet/config/navItems";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
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
  const { bg } = usePageBackground();
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
    <nav
      aria-label="Main"
      className={clsx(
        "group/nav absolute top-0 left-0 z-30 flex min-h-screen w-60 flex-col justify-between bg-surface-base text-text-primary-70",
        "laptop:absolute laptop:top-0 laptop:left-0 laptop:z-50 laptop:w-16 laptop:overflow-hidden laptop:hover:w-50 laptop:hover:border-r laptop:hover:border-core-primary-10 laptop:hover:bg-surface-base laptop:hover:shadow-lg",
        bg === "surface-5" ? "laptop:bg-surface-5 laptop:dark:bg-surface-base" : "laptop:bg-surface-base",
        "desktop:w-50 desktop:overflow-hidden desktop:border-r desktop:border-core-primary-10",
        bg === "surface-5" ? "desktop:bg-surface-5 desktop:dark:bg-surface-base" : "desktop:bg-surface-base",
        className,
      )}
    >
      <div className="flex flex-col items-start gap-1">
        {homeItem && homeItem.path ? (
          <div
            className={clsx("flex h-15 w-full items-start px-3 py-3 laptop:h-13 laptop:items-center laptop:!pb-0", {
              "border-b border-border-5": isPhone || isTablet,
            })}
          >
            <Link
              to={homeItem.path}
              aria-label="Home"
              className={clsx("flex items-center", {
                "w-full": isPhone || isTablet,
                "px-2.5": !(isPhone || isTablet),
              })}
            >
              {isPhone || isTablet ? (
                <Logo className="h-10 text-text-primary hover:cursor-pointer" />
              ) : (
                <div className="flex size-5 shrink-0 items-center justify-center">
                  <LogoAlt className="text-text-primary hover:cursor-pointer" />
                </div>
              )}
            </Link>
          </div>
        ) : null}

        <ul data-testid="navigation-menu" className="flex w-full flex-col items-start gap-1 px-3">
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
                  aria-label={item.label}
                  aria-current={isCurrentPath(item.path) ? "page" : undefined}
                  className={clsx(
                    "group flex h-10 w-full items-center rounded-lg px-2.5 py-2",
                    "hover:cursor-pointer hover:bg-core-primary-5",
                    isCurrentPath(item.path) || isPhone || isTablet ? "text-text-primary" : "text-text-primary-50",
                    { "bg-core-primary-5": isCurrentPath(item.path) },
                  )}
                >
                  <div className="flex size-5 shrink-0 items-center justify-center">
                    {item.icon
                      ? createElement(item.icon, {
                          className: "transition-transform duration-200 ease-gentle group-hover:scale-105",
                          width: "w-5",
                        })
                      : item.label}
                  </div>
                  {item.icon ? (
                    <span className="ml-3 text-emphasis-300 whitespace-nowrap text-text-primary-70 laptop:hidden laptop:group-hover/nav:inline desktop:inline">
                      {item.label}
                    </span>
                  ) : null}
                </Link>
              </li>
            ) : null;
          })}

          {/* On mobile/tablet: show expandable Settings menu */}
          {(isPhone || isTablet) &&
          settingsItem &&
          secondaryNavItems.filter((nav) => nav.parent === "/settings").length > 0 ? (
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
                  {settingsItem.icon
                    ? createElement(settingsItem.icon, {
                        className: "transition-transform duration-200 ease-gentle group-hover:scale-105",
                        width: "w-5",
                      })
                    : null}
                  <span className="ml-2 flex-1 text-left text-emphasis-300 text-text-primary-70">
                    {settingsItem.label}
                  </span>
                  {showSettingsHover || isSettingsExpanded ? (
                    <MorphingPlusMinus condition={showSettingsHover ? !isSettingsExpanded : false} />
                  ) : null}
                </button>
              </li>

              {/* Show secondary nav items when expanded */}
              <AnimatePresence>
                {isSettingsExpanded ? (
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
                            aria-current={isCurrentPath(nav.path) ? "page" : undefined}
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
                ) : null}
              </AnimatePresence>
            </>
          ) : null}
        </ul>
      </div>
      <div className="px-3 pb-3">
        <button
          onClick={() => {
            logout();
          }}
          aria-label="Log out"
          className={clsx(
            "group flex h-10 w-full items-center rounded-lg px-2.5 py-2",
            "hover:cursor-pointer hover:bg-core-primary-10",
          )}
          data-testid="logout-button"
        >
          <div className="flex size-5 shrink-0 items-center justify-center">
            <ArrowLeftCompact className="text-text-primary-50 transition-transform duration-200 ease-gentle group-hover:scale-105" />
          </div>
          <span className="ml-3 text-emphasis-300 whitespace-nowrap text-text-primary-70 laptop:hidden laptop:group-hover/nav:inline desktop:inline">
            Logout
          </span>
        </button>
      </div>
    </nav>
  );
};

export default Navigation;
