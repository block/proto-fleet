import { ReactNode, useEffect, useRef, useState } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import NavigationMenu from "../NavigationMenu";
import { ScheduleApiProvider } from "@/protoFleet/api/ScheduleApiProvider";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import PageHeader from "@/protoFleet/components/PageHeader";
import {
  getPhoneHeaderWidgetOffsetClass,
  getPhoneHeaderWidgetRowCount,
  getVisibleHeaderWidgetCount,
  PHONE_HEADER_WIDGET_HIDDEN_OFFSET_CLASS,
  shouldInlineFirstPhoneHeaderWidget,
  shouldStackPhoneHeaderWidgets,
} from "@/protoFleet/components/PageHeader/headerWidgetLayout";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";
import { useSchedulePillData } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { unscopedScopablePath } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type Props = {
  children: ReactNode;
};

const detailPagePathPatterns = [/^\/sites\/[^/]+$/, /^\/buildings\/[^/]+$/, /^\/racks\/[^/]+$/, /^\/groups\/[^/]+$/];

const isDetailPagePath = (pathname: string) => {
  const unscopedPath = unscopedScopablePath(pathname);
  return detailPagePathPatterns.some((pattern) => pattern.test(unscopedPath));
};

const AppLayoutContent = ({ children }: Props) => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const { pathname } = useLocation();
  const { bgClass } = usePageBackground();
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const schedulePillData = useSchedulePillData();
  const { activeEvent: activeCurtailmentEvent } = useCurtailmentPillData();
  const hasDismissedSetup = Boolean(dismissedSetup);
  const canReadCurtailment = useHasPermission("curtailment:read");
  const hasVisibleCurtailmentPill = activeCurtailmentEvent !== null && canReadCurtailment;
  const headerWidgetCount = getVisibleHeaderWidgetCount({
    hasDismissedSetup,
    hasVisibleCurtailmentPill,
    hasVisibleSchedules: schedulePillData.hasVisibleSchedules,
  });
  const inlineFirstPhoneWidget = isPhone && shouldInlineFirstPhoneHeaderWidget(headerWidgetCount);
  const phoneRowWidgetCount = getPhoneHeaderWidgetRowCount(headerWidgetCount, inlineFirstPhoneWidget);
  const stackPhoneWidgets = shouldStackPhoneHeaderWidgets(headerWidgetCount);

  const hideShellHeader = isDetailPagePath(pathname);
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
      <div className="fixed top-0 z-50 h-fit w-0 laptop:w-16 desktop:w-50">
        <NavigationMenu items={primaryNavItems} isVisible={isMenuOpen} closeMenu={() => setIsMenuOpen(false)} />
      </div>

      {hideShellHeader ? null : (
        <div
          className={`fixed top-0 right-0 bottom-[calc(100vh-theme(spacing.1)*12)] left-0 z-40 laptop:bottom-[calc(100vh-theme(spacing.1)*15)] laptop:left-16 desktop:left-50 ${bgClass}`}
        >
          <PageHeader
            activeCurtailmentEvent={activeCurtailmentEvent}
            isMenuOpen={isMenuOpen}
            openMenu={() => setIsMenuOpen(true)}
            schedulePillData={schedulePillData}
          />
        </div>
      )}

      <div
        ref={scrollRef}
        className={clsx(
          hideShellHeader
            ? "fixed top-0 right-0 bottom-0 left-0 z-20 laptop:left-16 desktop:left-50"
            : "fixed top-[calc(theme(spacing.1)*12)] right-0 bottom-0 left-0 z-20 laptop:top-[calc(theme(spacing.1)*15)] laptop:left-16 desktop:left-50",
          "overflow-x-hidden overflow-y-auto overscroll-x-none",
          bgClass,
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

const AppLayout = (props: Props) => (
  <ScheduleApiProvider>
    <SitesProvider>
      <AppLayoutContent {...props} />
    </SitesProvider>
  </ScheduleApiProvider>
);

export default AppLayout;
