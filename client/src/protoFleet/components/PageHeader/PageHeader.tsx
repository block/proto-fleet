import { type ReactElement } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import CurtailmentPill from "./CurtailmentPill";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import FirmwareRolloutPill from "./FirmwareRolloutPill";
import {
  getPhoneHeaderWidgetRowCount,
  getPhoneHeaderWidgetRowHeightClass,
  getVisibleHeaderWidgetCount,
  shouldInlineFirstPhoneHeaderWidget,
  shouldStackPhoneHeaderWidgets,
} from "./headerWidgetLayout";
import SchedulePill from "./SchedulePill";
import SitePicker from "./SitePicker";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import type { FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { scopedPath, unscopedScopablePath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { Menu } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface PageHeaderProps {
  activeCurtailmentEvent?: CurtailmentPillEvent | null;
  activeFirmwareRollouts?: FirmwareRollout[];
  isMenuOpen?: boolean;
  openMenu?: () => void;
  schedulePillData: UseSchedulePillDataResult;
}

interface HeaderWidgetsProps {
  activeCurtailmentEvent: CurtailmentPillEvent | null;
  align?: "start" | "end";
  activeFirmwareRollouts: FirmwareRollout[];
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
type HeaderWidgetKind = "curtailment" | "firmware" | "schedule" | "setup";

function HeaderWidgets({
  activeCurtailmentEvent,
  align = "start",
  activeFirmwareRollouts,
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
          case "firmware":
            return activeFirmwareRollouts.length > 0 ? (
              <FirmwareRolloutPill key={widget} rollouts={activeFirmwareRollouts} />
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
  activeFirmwareRollouts = [],
  isMenuOpen,
  openMenu,
  schedulePillData,
}: PageHeaderProps): ReactElement {
  const { isPhone, isTablet } = useWindowDimensions();
  const { bgClass } = usePageBackground();
  // The Dashboard renders its own heading-style site selector, so the topbar
  // picker is hidden there to avoid two selectors competing.
  const { pathname } = useLocation();
  const isDashboard = unscopedScopablePath(pathname) === "/dashboard";
  const [dismissedSetup, setDismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const hasDismissedSetup = Boolean(dismissedSetup);
  const canReadCurtailment = useHasPermission("curtailment:read");
  // ListSites is server-gated on org-scoped site:read; without it we skip the
  // fetch and hide the picker so non-site readers keep a clean header.
  const canReadSites = useHasPermission("site:read");

  // The site catalog is owned by the shell-level SitesProvider (one fetch +
  // poll shared with the routed pages), so the picker just reads it here.
  // `undefined` means "still loading" (the picker renders a skeleton); `[]`
  // means "no sites" (the picker hides itself unless `sitesError` is non-null,
  // in which case it shows the retry affordance).
  const { sites, sitesError, refetchSites } = useSitesContext();

  const handleCompleteSetup = () => {
    setDismissedSetup(false);
  };

  const headerWidgetsProps = {
    activeCurtailmentEvent,
    activeFirmwareRollouts,
    canReadCurtailment,
    dismissedSetup: hasDismissedSetup,
    onContinueSetup: handleCompleteSetup,
    schedulePillData,
  };
  const hasVisibleCurtailmentPill = activeCurtailmentEvent !== null && canReadCurtailment;
  const hasVisibleFirmwareRolloutPill = activeFirmwareRollouts.length > 0;
  const headerWidgetKinds: HeaderWidgetKind[] = [
    ...(hasVisibleCurtailmentPill ? (["curtailment"] as const) : []),
    ...(hasVisibleFirmwareRolloutPill ? (["firmware"] as const) : []),
    ...(schedulePillData.hasVisibleSchedules ? (["schedule"] as const) : []),
    ...(hasDismissedSetup ? (["setup"] as const) : []),
  ];
  const headerWidgetCount = getVisibleHeaderWidgetCount({
    hasDismissedSetup,
    hasVisibleCurtailmentPill,
    hasVisibleFirmwareRolloutPill,
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
              <Menu
                ariaExpanded={isMenuOpen}
                ariaLabel="Open navigation menu"
                className="mr-2 text-text-primary"
                onClick={openMenu}
                testId="navigation-menu-button"
              />
            ) : null}
            <div className="min-w-0 flex-1" data-testid="page-header-selector-area">
              {isDashboard || !canReadSites ? null : (
                <SitePicker sites={sites} error={sitesError} onRetry={refetchSites} />
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
