import { ColTitles } from "@/shared/components/List/types";
import { statuses } from "@/shared/components/StatusCircle";

export type TestItem = {
  id: string;
  name: string;
  status: string;
  value: number;
  timestamp: number;
  additionalField?: string;
};

export const testCols = {
  name: "name",
  status: "status",
  value: "value",
  timestamp: "timestamp",
};

export const testColTitles = {
  [testCols.name]: "Name",
  [testCols.status]: "Status",
  [testCols.value]: "Value",
  [testCols.timestamp]: "Time",
} as ColTitles<keyof TestItem>;

export const testFilterStates = {
  active: "active",
  inactive: "inactive",
  warning: "warning",
  error: "error",
};

export type TestFilterState =
  (typeof testFilterStates)[keyof typeof testFilterStates];

const now = new Date();

export const testItems: TestItem[] = [
  {
    id: "item1",
    name: "Test Item 1",
    status: testFilterStates.active,
    value: 100,
    timestamp: now.getTime(),
  },
  {
    id: "item2",
    name: "Test Item 2",
    status: testFilterStates.inactive,
    value: 200,
    timestamp: now.getTime() - 1000 * 60,
  },
  {
    id: "item3",
    name: "Test Item 3",
    status: testFilterStates.warning,
    value: 300,
    timestamp: now.getTime() - 1000 * 60 * 15,
  },
  {
    id: "item4",
    name: "Test Item 4",
    status: testFilterStates.error,
    value: 400,
    timestamp: now.getTime() - 1000 * 60 * 60,
    additionalField: "Extra data",
  },
  {
    id: "item5",
    name: "Test Item 5",
    status: testFilterStates.active,
    value: 500,
    timestamp: now.getTime() - 1000 * 60 * 60 * 2,
  },
];

export const testFilters = [
  {
    title: "All Items",
    value: "all",
    count: testItems.length,
  },
  {
    title: "Active",
    value: testFilterStates.active,
    count: testItems.filter((item) => item.status === testFilterStates.active)
      .length,
    status: statuses.normal,
  },
  {
    title: "Inactive",
    value: testFilterStates.inactive,
    count: testItems.filter((item) => item.status === testFilterStates.inactive)
      .length,
    status: statuses.inactive,
  },
  {
    title: "Warning",
    value: testFilterStates.warning,
    count: testItems.filter((item) => item.status === testFilterStates.warning)
      .length,
    status: statuses.warning,
  },
  {
    title: "Error",
    value: testFilterStates.error,
    count: testItems.filter((item) => item.status === testFilterStates.error)
      .length,
    status: statuses.error,
  },
];
