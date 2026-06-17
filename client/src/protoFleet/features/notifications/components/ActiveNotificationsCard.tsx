import HistoryTable from "./HistoryTable";

const ActiveNotificationsCard = () => (
  <section className="flex flex-col gap-4 rounded-xl bg-surface-base p-6 dark:bg-core-primary-5">
    <h3 className="text-heading-200">Active notifications</h3>
    <HistoryTable
      activeOnly
      noDataElement={<div className="py-6 text-center text-text-primary-50">No active notifications.</div>}
    />
  </section>
);

export default ActiveNotificationsCard;
