import { ReactNode } from "react";

import AppLayoutFrame from "./AppLayoutFrame";
import { ScheduleApiProvider } from "@/protoFleet/api/ScheduleApiProvider";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import PageHeader from "@/protoFleet/components/PageHeader";
import {
  getPhoneHeaderWidgetRowCount,
  getVisibleHeaderWidgetCount,
  shouldInlineFirstPhoneHeaderWidget,
  shouldStackPhoneHeaderWidgets,
} from "@/protoFleet/components/PageHeader/headerWidgetLayout";
import { useActiveAlertsPillData } from "@/protoFleet/components/PageHeader/useActiveAlertsPillData";
import { useCurtailmentPillData } from "@/protoFleet/components/PageHeader/useCurtailmentPillData";
import { useFleetNodeUpgradeIndicator } from "@/protoFleet/components/PageHeader/useFleetNodeUpgradeIndicator";
import { useRolloutPillData } from "@/protoFleet/components/PageHeader/useRolloutPillData";
import { useSchedulePillData } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { useUpdateIndicator } from "@/protoFleet/features/updates/useUpdateIndicator";
import { useHasPermission } from "@/protoFleet/store";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type Props = {
  children: ReactNode;
  hideShellHeader?: boolean;
};

const AppLayoutContent = ({ children, hideShellHeader = false }: Props) => {
  const { isPhone } = useWindowDimensions();
  const [dismissedSetup] = useReactiveLocalStorage<boolean>("completeSetupDismissed");
  const schedulePillData = useSchedulePillData();
  const { activeEvent: activeCurtailmentEvent } = useCurtailmentPillData();
  // Release discovery is an enhancement of the normal Fleet header. Focused
  // detail routes deliberately skip the poll along with the hidden header.
  const updatePill = useUpdateIndicator({ enabled: !hideShellHeader });
  const fleetNodeUpgradePill = useFleetNodeUpgradeIndicator({ enabled: !hideShellHeader });
  // Same deal for the active-alert poll: no header, no pill to feed.
  const activeAlertsPillData = useActiveAlertsPillData({ enabled: !hideShellHeader });
  const rolloutPillData = useRolloutPillData({ enabled: !hideShellHeader });
  const hasDismissedSetup = Boolean(dismissedSetup);
  const canReadCurtailment = useHasPermission("curtailment:read");
  const hasVisibleCurtailmentPill = activeCurtailmentEvent !== null && canReadCurtailment;
  const hasVisibleUpdatePill = updatePill !== null;
  const hasVisibleFleetNodeUpgradePill = fleetNodeUpgradePill !== null;
  const headerWidgetCount = getVisibleHeaderWidgetCount({
    hasDismissedSetup,
    hasVisibleAlertsPill: activeAlertsPillData.hasVisiblePill,
    hasVisibleFleetNodeUpgradePill,
    hasVisibleUpdatePill,
    hasVisibleCurtailmentPill,
    hasVisibleRolloutPill: rolloutPillData.hasVisiblePill,
    hasVisibleSchedules: schedulePillData.hasVisibleSchedules,
  });
  const inlineFirstPhoneWidget = isPhone && shouldInlineFirstPhoneHeaderWidget(headerWidgetCount);
  const phoneRowWidgetCount = getPhoneHeaderWidgetRowCount(headerWidgetCount, inlineFirstPhoneWidget);
  const stackPhoneWidgets = shouldStackPhoneHeaderWidgets(headerWidgetCount);

  return (
    <AppLayoutFrame
      hideShellHeader={hideShellHeader}
      phoneRowWidgetCount={phoneRowWidgetCount}
      stackPhoneWidgets={stackPhoneWidgets}
      renderHeader={(shell) => (
        <PageHeader
          {...shell}
          activeAlertsPillData={activeAlertsPillData}
          activeCurtailmentEvent={activeCurtailmentEvent}
          fleetNodeUpgradePill={fleetNodeUpgradePill}
          rolloutPillData={rolloutPillData}
          schedulePillData={schedulePillData}
          updatePill={updatePill}
        />
      )}
    >
      {children}
    </AppLayoutFrame>
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
