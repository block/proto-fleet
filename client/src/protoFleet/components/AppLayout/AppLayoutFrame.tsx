import { type ReactNode, useEffect, useRef, useState } from "react";
import clsx from "clsx";
import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import NavigationRailToggle from "@/protoFleet/components/NavigationMenu/NavigationRailToggle";
import { useNavigationRail } from "@/protoFleet/components/NavigationMenu/useNavigationRail";
import {
  getPhoneHeaderWidgetOffsetClass,
  PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS,
} from "@/protoFleet/components/PageHeader/headerWidgetLayout";
import { type NavItem, primaryNavItems } from "@/protoFleet/config/navItems";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { Menu } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export type ShellHeaderProps = { isMenuOpen: boolean; openMenu: () => void; navigationToggle: ReactNode };
type Props = {
  children: ReactNode;
  hideShellHeader?: boolean;
  items?: NavItem[];
  roleLabel?: string;
  phoneRowWidgetCount?: number;
  stackPhoneWidgets?: boolean;
  renderHeader: (props: ShellHeaderProps) => ReactNode;
};
const AppLayoutFrame = ({
  children,
  hideShellHeader = false,
  items = primaryNavItems,
  roleLabel,
  phoneRowWidgetCount = 0,
  stackPhoneWidgets = false,
  renderHeader,
}: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const rail = useNavigationRail();
  const { bgClass } = usePageBackground();
  const { isPhone } = useWindowDimensions();
  // Do not reopen a stale drawer after crossing into the persistent navigation rail.
  if (!isPhone && isMenuOpen) setIsMenuOpen(false);

  const showDetailMenuTrigger = hideShellHeader && (isPhone || rail.isCollapsible);
  const showPhoneWidgets = !hideShellHeader && isPhone && phoneRowWidgetCount > 0;

  // Publish the scroll container's vertical-scrollbar width as
  // `--content-scroll-gutter`. Some desktop page-scroll sticky chrome (see
  // PAGE_SCROLL_CHROME_WIDTH) sizes itself with `100vw`, which counts that
  // gutter while the client area does not — subtracting it keeps the chrome
  // pinned all the way to the end of a horizontal scroll. Resolves to 0 for
  // overlay scrollbars. Re-measured when the scroll area resizes (the gutter
  // appears/disappears as content overflows or the viewport changes).
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const update = () => el.style.setProperty("--content-scroll-gutter", `${el.offsetWidth - el.clientWidth}px`);
    update();
    const observer = typeof ResizeObserver !== "undefined" ? new ResizeObserver(update) : undefined;
    observer?.observe(el);
    return () => observer?.disconnect();
  }, []);

  return (
    <div className={clsx("absolute top-0 right-0 bottom-0 left-0 overflow-hidden", bgClass)}>
      <div className="fixed top-0 z-50 h-fit w-0 tablet:w-16 desktop:w-50">
        <NavigationMenu
          rail={rail}
          items={items}
          roleLabel={roleLabel}
          isVisible={isMenuOpen}
          closeMenu={() => setIsMenuOpen(false)}
        />
      </div>

      {showDetailMenuTrigger ? (
        <div
          className={clsx(
            "fixed top-0 left-0 z-40 flex h-12 items-center px-4 laptop:h-15",
            rail.isExpanded ? "tablet:left-50" : "tablet:left-16",
          )}
        >
          <NavigationRailToggle rail={rail} />
          {isPhone ? (
            <Menu
              ariaExpanded={isMenuOpen}
              ariaLabel="Open navigation menu"
              className="text-text-primary"
              onClick={() => setIsMenuOpen(true)}
              testId="navigation-menu-button"
            />
          ) : null}
        </div>
      ) : null}

      {hideShellHeader ? null : (
        <div
          data-testid="app-header"
          className={`fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*12)] left-0 z-40 ${rail.isExpanded ? "tablet:left-50" : "tablet:left-16"} laptop:bottom-[calc(100vh-theme(spacing.1)*15)] desktop:left-50 ${bgClass}`}
        >
          {renderHeader({
            isMenuOpen,
            openMenu: () => setIsMenuOpen(true),
            navigationToggle: <NavigationRailToggle rail={rail} />,
          })}
        </div>
      )}

      <div
        ref={scrollRef}
        data-testid="app-content"
        className={clsx(
          hideShellHeader
            ? "fixed top-0 right-0 bottom-0 left-0 z-20 tablet:left-16 desktop:left-50"
            : "fixed top-[calc(theme(spacing.1)*12)] right-0 bottom-0 left-0 z-20 tablet:left-16 laptop:top-[calc(theme(spacing.1)*15)] desktop:left-50",
          "overflow-y-auto phone:overflow-x-hidden phone:overscroll-x-none tablet-only:overflow-x-hidden tablet-only:overscroll-x-none",
          bgClass,
          showDetailMenuTrigger && "pt-12 laptop:pt-15",
          !hideShellHeader &&
            (showPhoneWidgets
              ? getPhoneHeaderWidgetOffsetClass(phoneRowWidgetCount, stackPhoneWidgets)
              : PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS),
        )}
      >
        {children}
      </div>
    </div>
  );
};

export default AppLayoutFrame;
