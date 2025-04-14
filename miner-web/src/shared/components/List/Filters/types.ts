import { Key } from "react";
import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export type FilterItem<FilterType extends Key> = {
  title: string;
  value: FilterType;
  status?: StatusCircleStatus;
  count: number;
};
