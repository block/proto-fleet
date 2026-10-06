import { type ReactNode, useEffect, useRef } from "react";

import SettingsPageHeader from "./SettingsPageHeader";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

export type FirmwareTab = "files" | "releaseChannels";

type Props = {
  activeTab: FirmwareTab;
  onSelectTab: (tab: FirmwareTab) => void;
  manageRequest: { channelId: bigint } | null;
  monitor: ReactNode;
  headerAction?: ReactNode;
  refreshWarning?: ReactNode;
  children: ReactNode;
};

const FirmwarePageLayout = ({
  activeTab,
  onSelectTab,
  manageRequest,
  monitor,
  headerAction,
  refreshWarning,
  children,
}: Props) => {
  const tabNavigationRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (activeTab === "releaseChannels" && manageRequest) {
      tabNavigationRef.current?.scrollIntoView({ block: "start", behavior: "instant" });
    }
  }, [activeTab, manageRequest]);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader title="Firmware" />
        {headerAction}
      </div>
      {refreshWarning}
      {monitor}
      <div ref={tabNavigationRef} className="scroll-mt-6" data-testid="firmware-tab-navigation">
        <TabStrip
          activeId={activeTab}
          ariaLabel="Firmware sections"
          onSelect={(tab) => onSelectTab(tab === "releaseChannels" ? "releaseChannels" : "files")}
        >
          <TabStripItem id="files" label="Files" />
          <TabStripItem id="releaseChannels" label="Release channels" />
        </TabStrip>
      </div>
      {children}
    </div>
  );
};

export default FirmwarePageLayout;
