import { type ReactElement, useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import CurtailmentPill from "./CurtailmentPill";
import type { CurtailmentPillEvent } from "./curtailmentPillTypes";
import FirmwareRolloutPill from "./FirmwareRolloutPill";
import LocationSelector from "./LocationSelector";
import SchedulePill from "./SchedulePill";
import SitePicker from "./SitePicker";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import { type FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { MULTI_SITE_ENABLED } from "@/protoFleet/constants/featureFlags";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import { useHasPermission } from "@/protoFleet/store";
import { Pause } from "@/shared/assets/icons";
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
  activeFirmwareRollouts: FirmwareRollout[];
  canReadCurtailment: boolean;
  className?: string;
  dismissedSetup: boolean;
  onContinueSetup: () => void;
  schedulePillData: UseSchedulePillDataResult;
}

const headerWidgetEnabled = true;

function HeaderWidgets({
  activeCurtailmentEvent,
  activeFirmwareRollouts,
  canReadCurtailment,
  className,
  dismissedSetup,
  onContinueSetup,
  schedulePillData,
}: HeaderWidgetsProps): ReactElement {
  const { pillSchedule, sections, pendingScheduleId, onToggleScheduleStatus } = schedulePillData;

  return (
    <div className={clsx("flex space-x-3", className)}>
      {activeCurtailmentEvent && canReadCurtailment ? (
        <CurtailmentPill event={activeCurtailmentEvent} detailsPath="/energy" />
      ) : null}
      {activeFirmwareRollouts.length > 0 ? <FirmwareRolloutPill rollouts={activeFirmwareRollouts} /> : null}
      {pillSchedule ? (
        <SchedulePill
          pillSchedule={pillSchedule}
          sections={sections}
          pendingScheduleId={pendingScheduleId}
          onToggleScheduleStatus={onToggleScheduleStatus}
        />
      ) : null}
      {dismissedSetup ? (
        <Button variant={variants.secondary} size={sizes.compact} text="Continue setup" onClick={onContinueSetup} />
      ) : null}
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
    activeFirmwareRollouts,
    canReadCurtailment,
    dismissedSetup: hasDismissedSetup,
    onContinueSetup: handleCompleteSetup,
    schedulePillData,
  };
  const hasVisibleCurtailmentPill = activeCurtailmentEvent !== null && canReadCurtailment;
  const hasVisibleFirmwareRolloutPill = activeFirmwareRollouts.length > 0;
  const showPhoneWidgets =
    isPhone &&
    (hasDismissedSetup ||
      schedulePillData.hasVisibleSchedules ||
      hasVisibleCurtailmentPill ||
      hasVisibleFirmwareRolloutPill);

  return (
    <>
      <div className="flex h-12 items-center laptop:h-15">
        <div className="flex grow items-center px-4">
          <div className="flex grow items-center">
            {isPhone || isTablet ? (
              <Pause
                ariaExpanded={isMenuOpen}
                ariaLabel="Open navigation menu"
                className="mr-2 text-text-primary"
                onClick={openMenu}
                testId="navigation-menu-button"
              />
            ) : null}
            {MULTI_SITE_ENABLED ? (
              <SitePicker sites={sites} error={sitesError} onRetry={fetchSites} />
            ) : (
              <LocationSelector />
            )}
          </div>
          {!isPhone && headerWidgetEnabled ? <HeaderWidgets {...headerWidgetsProps} /> : null}
        </div>
      </div>
      {showPhoneWidgets ? (
        <div className={clsx("flex h-[57px] items-center", bgClass)}>
          <HeaderWidgets className="ml-5" {...headerWidgetsProps} />
        </div>
      ) : null}
    </>
  );
}

export default PageHeader;
