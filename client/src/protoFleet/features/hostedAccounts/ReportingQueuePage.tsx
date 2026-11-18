import { AccountLayout } from "./AccountLayout";
import ReportingQueue from "./components/ReportingQueue";
import { SitesProvider } from "@/protoFleet/api/SitesProvider";
import { useFleetStore } from "@/protoFleet/store";

const ReportingQueuePage = () => {
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  return (
    <SitesProvider>
      <AccountLayout
        items={[
          { path: "/accounts", label: "Accounts", exact: true },
          { path: "/accounts/reports", label: "Reports", exact: true },
        ]}
        roleLabel="Reporting"
      >
        <div className="flex min-w-0 flex-col gap-6 p-10 phone:p-6">
          <ReportingQueue key={generation} />
        </div>
      </AccountLayout>
    </SitesProvider>
  );
};
export default ReportingQueuePage;
