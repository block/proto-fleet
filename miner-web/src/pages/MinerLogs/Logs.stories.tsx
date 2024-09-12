import { mockLogs } from "./constants";
import LogsComponent from "./Logs";

export const Logs = () => {
  return <div className="ml-4"><LogsComponent logsData={mockLogs} /></div>;
};

export default {
  title: "Components/Logs",
};
