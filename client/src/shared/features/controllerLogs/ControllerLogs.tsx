import { useState } from "react";

import AuditTrailView from "./AuditTrailView";
import ControllerLogView from "./ControllerLogView";
import type { ControllerRecordsSource } from "./types";
import SegmentedControl from "@/shared/components/SegmentedControl";

/**
 * A CT1 controller's own logs and audit trail, read through the app's source.
 * The audit trail is offered only when the source can read it.
 */
const ControllerLogs = ({ readLogs, listAuditEvents, pollIntervalMs }: ControllerRecordsSource) => {
  const [view, setView] = useState("logs");
  return (
    <>
      {listAuditEvents ? (
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
      ) : null}
      {view === "logs" || !listAuditEvents ? (
        <ControllerLogView readLogs={readLogs} pollIntervalMs={pollIntervalMs} />
      ) : (
        <AuditTrailView listAuditEvents={listAuditEvents} pollIntervalMs={pollIntervalMs} />
      )}
    </>
  );
};

export default ControllerLogs;
