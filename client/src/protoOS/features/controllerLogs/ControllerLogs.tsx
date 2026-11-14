import { useState } from "react";

import AuditTrailView from "./AuditTrailView";
import ControllerLogView from "./ControllerLogView";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import SegmentedControl from "@/shared/components/SegmentedControl";

const ControllerLogs = ({ adapter }: { adapter: MinerAdapter }) => {
  const [view, setView] = useState("logs");
  return (
    <>
      <div className="px-4 py-3">
        <SegmentedControl
          segments={[
            { key: "logs", title: "Logs" },
            { key: "audit", title: "Audit trail" },
          ]}
          initialSegmentKey={view}
          onSelect={setView}
        />
      </div>
      {view === "logs" ? <ControllerLogView adapter={adapter} /> : <AuditTrailView adapter={adapter} />}
    </>
  );
};

export default ControllerLogs;
