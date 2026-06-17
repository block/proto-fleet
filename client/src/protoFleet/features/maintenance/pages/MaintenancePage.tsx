import { useCallback, useState } from "react";

import TicketQueue from "../components/TicketQueue/TicketQueue";
import HistoryTab from "../components/TicketHistory/HistoryTab";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

type MaintenanceTabId = "queue" | "history";

const MaintenancePage = () => {
  const [activeTab, setActiveTab] = useState<MaintenanceTabId>("queue");

  const handleTabSelect = useCallback((id: string) => {
    setActiveTab(id as MaintenanceTabId);
  }, []);

  return (
    <div className="flex h-full flex-col">
      <div className="sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-6 laptop:px-10">
        <h1 className="text-heading-300 text-text-primary">Maintenance</h1>
        <TabStrip activeId={activeTab} onSelect={handleTabSelect} ariaLabel="Maintenance sections">
          <TabStripItem id="queue" label="Queue" testId="maintenance-tab-queue" />
          <TabStripItem id="history" label="History" testId="maintenance-tab-history" />
        </TabStrip>
      </div>
      <div className="min-h-0 flex-1 px-6 pt-6 laptop:px-10">
        {activeTab === "queue" && <TicketQueue />}
        {activeTab === "history" && <HistoryTab />}
      </div>
    </div>
  );
};

export default MaintenancePage;
