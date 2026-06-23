import HistoryTable from "./HistoryTable";
import Header from "@/shared/components/Header";

const HistorySection = () => (
  <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
    <Header title="History" titleSize="text-heading-200" />
    <p className="text-300 text-text-primary-50">
      Chronological record of notifications delivered by the rule engine, most recent first.
    </p>
    <HistoryTable
      noDataElement={
        <div className="py-10 text-center text-text-primary-50">
          No notifications delivered yet — alerts will appear here as they fire.
        </div>
      }
    />
  </section>
);

export default HistorySection;
