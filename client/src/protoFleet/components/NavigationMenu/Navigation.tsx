import { AnimatePresence, motion } from "motion/react";
import { createElement, useCallback, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import clsx from "clsx";
import type { NavigationRail } from "./useNavigationRail";
import { useLogoutAction } from "@/protoFleet/api/useLogout";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { isNavItemAllowedByPermissions, NavItem, secondaryNavItems } from "@/protoFleet/config/navItems";
import { formatRole } from "@/protoFleet/features/settings/utils/formatRole";
import { useNavFeatureEnabled } from "@/protoFleet/hooks/useNavFeatureEnabled";
import { scopedPath, unscopedScopablePath } from "@/protoFleet/routing/siteScope";
import { usePermissions, useRole, useUsername } from "@/protoFleet/store";
import { ArrowLeftCompact, LogoAlt } from "@/shared/assets/icons";
import MorphingPlusMinus from "@/shared/components/MorphingPlusMinus";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";
import { stripLeadingSlash } from "@/shared/utils/stringUtils";

type NavigationProps = {
  items: NavItem[];
  className?: string;
  isFloatingMenu?: boolean;
  rail?: NavigationRail;
  closeMenu?: () => void;
};

const Navigation = ({ items, className, closeMenu, isFloatingMenu = false, rail }: NavigationProps) => {
  const { pathname } = useLocation();
  const isExpanded = rail?.isExpanded ?? false;
  const handleNavigate = () => {
    rail?.collapse();
    closeMenu?.();
  };
  const logout = useLogoutAction();
  const username = useUsername();
  const role = formatRole(useRole());
  const accountIdentity = role ? `${username} · ${role}` : username;
  const permissions = usePermissions();
  const featureEnabled = useNavFeatureEnabled();
  const { activeSite } = useActiveSite({});
  // Site-scoped links resolve the slug via ResolveSiteBySlug (site:read-gated),
  // which bounces a role without site:read. Such a role has no meaningful site
  // scope, so build unscoped links for it (e.g. Fleet reached via miner:read).
  const canScopeToSite = permissions.includes("site:read");
  const scopeLink = (item: Pick<NavItem, "path" | "scopable">) =>
    item.scopable && canScopeToSite ? scopedPath(item.path, activeSite) : item.path;
  const [settingsManuallyToggled, setSettingsManuallyToggled] = useState(false);
  const visibleItems = useMemo(
    () => items.filter((item) => isNavItemAllowedByPermissions(item, permissions)),
    [items, permissions],
  );
  const [showSettingsHover, setShowSettingsHover] = useState(false);

  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  const homeItem = useMemo(() => items.find((item) => item.label === "Home"), [items]);
  const settingsItem = useMemo(() => items.find((item) => item.label === "Settings"), [items]);
  const visibleSettingsItems = useMemo(
    () =>
      secondaryNavItems
        .filter((nav) => nav.parent === "/settings")
        .filter((nav) => isNavItemAllowedByPermissions(nav, permissions))
        .filter((nav) => !nav.requiredFeature || featureEnabled[nav.requiredFeature]),
    [featureEnabled, permissions],
  );
  const visibleSettingsGroups = useMemo(
    () =>
      visibleSettingsItems.reduce<Array<{ section?: string; items: typeof visibleSettingsItems }>>((groups, item) => {
        const lastGroup = groups[groups.length - 1];
        if (lastGroup && lastGroup.section === item.section) {
          lastGroup.items.push(item);
          return groups;
        }

        groups.push({ section: item.section, items: [item] });
        return groups;
      }, []),
    [visibleSettingsItems],
  );

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

  const isCurrentPath = (item: string | Pick<NavItem, "path" | "scopable">) => {
    if (typeof item === "string") {
      const _pathname = stripLeadingSlash(pathname);
      const _path = stripLeadingSlash(item);
      return _pathname === _path || _pathname.startsWith(`${_path}/`);
    }

    const _pathname = stripLeadingSlash(item.scopable ? unscopedScopablePath(pathname) : pathname);
    const path = item.path;
    const _path = stripLeadingSlash(path);
    return _pathname === _path || _pathname.startsWith(`${_path}/`);
  };

  return (
    <nav
      ref={rail?.navigationRef}
      id={rail?.navigationId}
      aria-label="Main"
      className={clsx(
        "absolute top-0 left-0 z-30 flex h-dvh max-h-dvh min-h-0 w-60 flex-col justify-between overflow-hidden bg-surface-base text-text-primary-70",
        !isFloatingMenu && (isExpanded ? "tablet:w-50" : "tablet:w-16 desktop:w-50"),
        !isFloatingMenu && "desktop:border-r desktop:border-core-primary-10 desktop:hover:shadow-lg",
        isExpanded && "border-r border-core-primary-10 shadow-lg",
        className,
      )}
    >
      <div className="flex min-h-0 flex-1 flex-col items-start gap-1">
        {homeItem && homeItem.path ? (
          <div
            className={clsx(
              "flex h-15 w-full shrink-0 items-center px-3 py-3",
              !isFloatingMenu && "tablet:h-12 tablet:py-0 laptop:h-15 desktop:h-13 desktop:pt-3",
              {
                "border-b border-border-5": isFloatingMenu,
              },
            )}
          >
            <Link
              to={scopeLink(homeItem)}
              aria-label="Home"
              onClick={handleNavigate}
              className="flex items-center px-2.5"
            >
              <div className="flex size-5 shrink-0 items-center justify-center">
                <LogoAlt testId="navigation-logo" className="text-text-primary hover:cursor-pointer" />
              </div>
            </Link>
          </div>
        ) : null}

        <ul
          data-testid="navigation-menu"
          className={clsx(
            "flex min-h-0 w-full flex-1 flex-col items-start gap-1 overflow-y-auto overscroll-contain px-3",
            isFloatingMenu && "pb-3",
          )}
        >
          {visibleItems.map((item) => {
            // In the drawer, render Settings separately with expandable secondary navigation.
            if (
              isFloatingMenu &&
              item.path === "/settings" &&
              secondaryNavItems.some((nav) => nav.parent === item.path)
            ) {
              return null;
            }

            return item.path ? (
              <li key={item.path} className="w-full">
                <Link
                  to={scopeLink(item)}
                  onClick={handleNavigate}
                  aria-label={item.label}
                  aria-current={isCurrentPath(item) ? "page" : undefined}
                  className={clsx(
                    "group flex h-10 w-full items-center rounded-lg px-2.5 py-2",
                    "hover:cursor-pointer hover:bg-core-primary-5",
                    isCurrentPath(item) || isFloatingMenu ? "text-text-primary" : "text-text-primary-50",
                    { "bg-core-primary-5": isCurrentPath(item) },
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
                    <span
                      className={clsx(
                        "ml-3 text-emphasis-300 whitespace-nowrap text-text-primary-70",
                        !isFloatingMenu && !isExpanded && "tablet:hidden desktop:inline",
                      )}
                    >
                      {item.label}
                    </span>
                  ) : null}
                </Link>
              </li>
            ) : null;
          })}

          {/* In the drawer, show an expandable Settings menu. */}
          {isFloatingMenu && settingsItem && visibleSettingsItems.length > 0 ? (
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
                    "group flex h-10 w-full items-center justify-start rounded-lg px-2.5 py-2 text-text-primary",
                    "hover:cursor-pointer hover:bg-core-primary-5",
                  )}
                >
                  <div className="flex size-5 shrink-0 items-center justify-center">
                    {settingsItem.icon
                      ? createElement(settingsItem.icon, {
                          className: "transition-transform duration-200 ease-gentle group-hover:scale-105",
                          width: "w-5",
                        })
                      : null}
                  </div>
                  <span className="ml-3 flex-1 text-left text-emphasis-300 text-text-primary-70">
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
                    className="flex w-full flex-col gap-5"
                  >
                    {visibleSettingsGroups.map((group, index) => (
                      <div key={group.section ?? `settings-group-${index}`} className="flex w-full flex-col">
                        {group.section ? (
                          <div className="px-9 text-200 font-medium text-text-primary-50">{group.section}</div>
                        ) : null}
                        {group.items.map((nav) => (
                          <li key={nav.path} className="w-full">
                            <Link
                              to={nav.path}
                              onClick={handleNavigate}
                              aria-current={isCurrentPath(nav.path) ? "page" : undefined}
                              className={clsx(
                                "flex h-10 items-center rounded-lg px-9 text-emphasis-300 text-text-primary-70",
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
                      </div>
                    ))}
                  </motion.div>
                ) : null}
              </AnimatePresence>
            </>
          ) : null}
        </ul>
      </div>
      <div
        className="mx-3 mb-3 flex shrink-0 items-center border-t border-border-5 pt-3"
        role="group"
        aria-label={accountIdentity}
        title={accountIdentity}
      >
        <div
          className={clsx(
            "min-w-0 flex-1 py-2 pr-2 pl-2.5",
            !isFloatingMenu && !isExpanded && "tablet:hidden desktop:block",
          )}
        >
          <div className="truncate text-emphasis-300 text-text-primary-70">{username}</div>
          {role ? <div className="truncate text-200 text-text-primary-50">{role}</div> : null}
        </div>
        <button
          type="button"
          onClick={() => {
            logout();
          }}
          aria-label="Log out"
          title="Log out"
          className={clsx(
            "group flex size-10 shrink-0 items-center justify-center rounded-lg",
            "hover:cursor-pointer hover:bg-core-primary-10",
          )}
          data-testid="logout-button"
        >
          <div className="flex size-5 shrink-0 items-center justify-center">
            <ArrowLeftCompact className="text-text-primary-50 transition-transform duration-200 ease-gentle group-hover:scale-105" />
          </div>
        </button>
      </div>
    </nav>
  );
};

export default Navigation;
