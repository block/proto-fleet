import { useCallback, useState } from "react";
import HistoryTable from "./HistoryTable";

const ActiveNotificationsCard = () => {
  // The dashboard gate is a flat permission union; a site-scoped notification:read grant reaches here but
  // is denied the org-scoped history RPC, so drop the card on that denial rather than poll it forever.
  const [denied, setDenied] = useState(false);
  const handleDenied = useCallback(() => setDenied(true), []);

  if (denied) return null;

  return (
    <section className="flex flex-col gap-4 rounded-xl bg-surface-base p-6 dark:bg-core-primary-5">
      <h3 className="text-heading-200">Active notifications</h3>
      <HistoryTable
        activeOnly
        onPermissionDenied={handleDenied}
        noDataElement={<div className="py-6 text-center text-text-primary-50">No active notifications.</div>}
      />
    </section>
  );
};

export default ActiveNotificationsCard;
