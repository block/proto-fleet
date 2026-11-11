import { type ReactNode, useEffect, useRef } from "react";

import SettingsPageHeader from "./SettingsPageHeader";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

export type FirmwareTab = "files" | "releaseChannels" | "frigatePackages";

type Props = {
  activeTab: FirmwareTab;
  onSelectTab: (tab: FirmwareTab) => void;
  manageRequest: { channelId: bigint } | null;
  monitor: ReactNode;
  headerAction?: ReactNode;
  /** Offers the Frigate packages tab, which needs the controller software update permission. */
  showFrigatePackages?: boolean;
  /** Offers the miner Files and Release channels tabs, which need the miner firmware permission. */
  showMinerFirmware?: boolean;
  refreshWarning?: ReactNode;
  children: ReactNode;
};

const FirmwarePageLayout = ({
  activeTab,
  onSelectTab,
  manageRequest,
  monitor,
  headerAction,
  showFrigatePackages = false,
  showMinerFirmware = true,
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
          onSelect={(tab) =>
            onSelectTab(
              tab === "releaseChannels" ? "releaseChannels" : tab === "frigatePackages" ? "frigatePackages" : "files",
            )
          }
        >
          {showMinerFirmware ? <TabStripItem id="files" label="Files" /> : null}
          {showMinerFirmware ? <TabStripItem id="releaseChannels" label="Release channels" /> : null}
          {showFrigatePackages ? <TabStripItem id="frigatePackages" label="Frigate packages" /> : null}
        </TabStrip>
      </div>
      {children}
    </div>
  );
};

export default FirmwarePageLayout;
