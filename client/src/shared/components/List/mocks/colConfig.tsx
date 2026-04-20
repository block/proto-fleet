/* eslint-disable react-refresh/only-export-components */
import { testCols, TestItem } from "./data";
import { ColConfig } from "@/shared/components/List/types";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

const TestItemStatus = ({ status }: { status: string }) => {
  let statusType;

  switch (status) {
    case "active":
      statusType = statuses.normal;
      break;
    case "inactive":
      statusType = statuses.inactive;
      break;
    case "warning":
      statusType = statuses.warning;
      break;
    case "error":
      statusType = statuses.error;
      break;
    default:
      statusType = statuses.normal;
  }

  return <StatusCircle status={statusType} />;
};

const ValueDisplay = ({ value }: { value: number }) => {
  return (
    <div className="flex items-center">
      <span>{value}</span>
    </div>
  );
};

const testColConfig: ColConfig<TestItem, TestItem["id"]> = {
  [testCols.name]: {
    width: "w-24",
  },
  [testCols.status]: {
    component: (item: TestItem) => <TestItemStatus status={item.status} />,
    width: "w-17",
  },
  [testCols.value]: {
    component: (item: TestItem) => <ValueDisplay value={item.value} />,
    width: "w-38",
  },
  [testCols.timestamp]: {
    component: (item: TestItem) => (
      <div className="text-text-primary-50">{getRelativeTimeFromEpoch(item.timestamp)}</div>
    ),
    width: "w-24",
  },
};

export default testColConfig;
