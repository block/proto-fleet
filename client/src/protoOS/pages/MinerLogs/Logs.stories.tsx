import { mockLogs } from "./constants";
import LogsComponent from "./Logs";

export const Logs = () => {
  return (
    <div className="-mt-4 ml-4 w-[calc(100%-240px)]">
      <LogsComponent logsData={mockLogs} fetchMaxLogs={() => Promise.resolve(mockLogs)} />
    </div>
  );
};

export default {
  title: "ProtoOS/Logs",
};
