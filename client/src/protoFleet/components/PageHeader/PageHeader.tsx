import { type ReactElement, useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import CurtailmentPill from "./CurtailmentPill";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import {
  getPhoneHeaderWidgetRowCount,
  getPhoneHeaderWidgetRowHeightClass,
  getVisibleHeaderWidgetCount,
  shouldInlineFirstPhoneHeaderWidget,
  shouldStackPhoneHeaderWidgets,
} from "./headerWidgetLayout";
import LocationSelector from "./LocationSelector";
import SchedulePill from "./SchedulePill";
import SitePicker from "./SitePicker";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { scopedPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { Pause } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface PageHeaderProps {
  activeCurtailmentEvent?: CurtailmentPillEvent | null;
  isMenuOpen?: boolean;
  openMenu?: () => void;
  schedulePillData: UseSchedulePillDataResult;
}

interface HeaderWidgetsProps {
  activeCurtailmentEvent: CurtailmentPillEvent | null;
  align?: "start" | "end";
  canReadCurtailment: boolean;
  className?: string;
  dismissedSetup: boolean;
  onContinueSetup: () => void;
  schedulePillData: UseSchedulePillDataResult;
  stacked?: boolean;
  testId?: string;
  widgets: HeaderWidgetKind[];
}

const headerWidgetEnabled = true;
type HeaderWidgetKind = "curtailment" | "schedule" | "setup";

function HeaderWidgets({
  activeCurtailmentEvent,
  align = "start",
  canReadCurtailment,
  className,
  dismissedSetup,
  onContinueSetup,
  schedulePillData,
  stacked = false,
  testId,
  widgets,
}: HeaderWidgetsProps): ReactElement {
  const { pillSchedule, sections, pendingScheduleId, onToggleScheduleStatus } = schedulePillData;
  const alignEnd = align === "end";
  const storedActiveSite = useFleetStore((state) => state.ui.activeSite);
  const routeScope = useRouteSiteScope();
  const energyPath = scopedPath("/energy", routeScope ?? storedActiveSite);

  return (
    <div
      className={clsx(
        "flex",
        stacked ? "flex-col gap-2" : "items-center gap-3",
        alignEnd && !stacked && "justify-end",
        stacked && (alignEnd ? "items-end" : "items-start"),
        className,
      )}
      data-testid={testId}
    >
      {widgets.map((widget) => {
        switch (widget) {
          case "curtailment":
            return activeCurtailmentEvent && canReadCurtailment ? (
              <CurtailmentPill key={widget} event={activeCurtailmentEvent} detailsPath={energyPath} />
            ) : null;
          case "schedule":
            return pillSchedule ? (
              <SchedulePill
                key={widget}
                pillSchedule={pillSchedule}
                sections={sections}
                pendingScheduleId={pendingScheduleId}
                onToggleScheduleStatus={onToggleScheduleStatus}
              />
            ) : null;
          case "setup":
            return dismissedSetup ? (
              <Button
                key={widget}
                className="max-w-full min-w-0 overflow-hidden"
                variant={variants.secondary}
                size={sizes.compact}
                onClick={onContinueSetup}
              >
                <span className="block min-w-0 truncate">Continue setup</span>
              </Button>
            ) : null;
        }
      })}
    </div>
  );
}

function PageHeader({
  activeCurtailmentEvent = null,
  isMenuOpen,
  openMenu,
  schedulePillData,
}: PageHeaderProps): ReactElement {
  const { isPhone, isTablet } = useWindowDimensions();
  const { bgClass } = usePageBackground();
  const [dismissedSetup, setDismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const hasDismissedSetup = Boolean(dismissedSetup);
  const canReadCurtailment = useHasPermission("curtailment:read");

  // Multi-site: the SitePicker replaces today's LocationSelector when the
  // feature flag is on. Sites are fetched once on mount and held here so the
  // picker doesn't re-fire ListSites on every route change. `undefined`
  // means "still loading" (the picker renders a skeleton); `[]` means "no
  // sites" (the picker hides itself unless `sitesError` is non-null, in
  // which case it shows the retry affordance).
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(MULTI_SITE_ENABLED ? undefined : []);
  const [sitesError, setSitesError] = useState<string | null>(null);

  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setSitesError(null);
      },
      onError: (msg) => {
        setSitesError(msg);
        setSites([]);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  useEffect(() => {
    if (!MULTI_SITE_ENABLED) return;
    return fetchSites();
  }, [fetchSites]);

  const handleCompleteSetup = () => {
    setDismissedSetup(false);
  };

  const headerWidgetsProps = {
    activeCurtailmentEvent,
    canReadCurtailment,
    dismissedSetup: hasDismissedSetup,
    onContinueSetup: handleCompleteSetup,
    schedulePillData,
  };
  const hasVisibleCurtailmentPill = activeCurtailmentEvent !== null && canReadCurtailment;
  const headerWidgetKinds: HeaderWidgetKind[] = [
    ...(hasVisibleCurtailmentPill ? (["curtailment"] as const) : []),
    ...(schedulePillData.hasVisibleSchedules ? (["schedule"] as const) : []),
    ...(hasDismissedSetup ? (["setup"] as const) : []),
  ];
  const headerWidgetCount = getVisibleHeaderWidgetCount({
    hasDismissedSetup,
    hasVisibleCurtailmentPill,
    hasVisibleSchedules: schedulePillData.hasVisibleSchedules,
  });
  const inlineFirstPhoneWidget = isPhone && shouldInlineFirstPhoneHeaderWidget(headerWidgetCount);
  const phoneTopWidgetKinds = inlineFirstPhoneWidget ? headerWidgetKinds.slice(0, 1) : [];
  const phoneRowWidgetKinds = inlineFirstPhoneWidget ? headerWidgetKinds.slice(1) : headerWidgetKinds;
  const phoneRowWidgetCount = getPhoneHeaderWidgetRowCount(headerWidgetCount, inlineFirstPhoneWidget);
  const stackPhoneWidgets = shouldStackPhoneHeaderWidgets(headerWidgetCount);
  const showPhoneWidgets = isPhone && phoneRowWidgetCount > 0;

  return (
    <>
      <div className="flex h-12 items-center laptop:h-15">
        <div
          className={clsx(
            "w-full px-4",
            inlineFirstPhoneWidget
              ? "grid grid-cols-[minmax(0,1fr)_minmax(0,min(15rem,45vw))] items-center gap-3"
              : "flex grow items-center",
          )}
          data-testid="page-header-content"
        >
          <div
            className={clsx("flex min-w-0 items-center", !inlineFirstPhoneWidget && "flex-1")}
            data-testid="page-header-location-area"
          >
            {isPhone || isTablet ? (
              <Pause
                ariaExpanded={isMenuOpen}
                ariaLabel="Open navigation menu"
                className="mr-2 text-text-primary"
                onClick={openMenu}
                testId="navigation-menu-button"
              />
            ) : null}
            <div className="min-w-0 flex-1" data-testid="page-header-selector-area">
              {MULTI_SITE_ENABLED ? (
                <SitePicker sites={sites} error={sitesError} onRetry={fetchSites} />
              ) : (
                <LocationSelector />
              )}
            </div>
          </div>
          {!isPhone && headerWidgetEnabled ? (
            <HeaderWidgets testId="page-header-desktop-widgets" widgets={headerWidgetKinds} {...headerWidgetsProps} />
          ) : null}
          {inlineFirstPhoneWidget ? (
            <HeaderWidgets
              className="min-w-0 justify-end overflow-hidden"
              testId="page-header-inline-widgets"
              widgets={phoneTopWidgetKinds}
              {...headerWidgetsProps}
            />
          ) : null}
        </div>
      </div>
      {showPhoneWidgets ? (
        <div
          className={clsx(
            "flex items-start justify-end px-4",
            getPhoneHeaderWidgetRowHeightClass(phoneRowWidgetCount, stackPhoneWidgets),
            bgClass,
          )}
          data-testid="phone-header-widget-row"
        >
          <HeaderWidgets
            align="end"
            stacked={stackPhoneWidgets}
            testId="page-header-mobile-widgets"
            widgets={phoneRowWidgetKinds}
            {...headerWidgetsProps}
          />
        </div>
      ) : null}
    </>
  );
}

export default PageHeader;
