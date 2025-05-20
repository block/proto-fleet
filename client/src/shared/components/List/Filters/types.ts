import { Key } from "react";
import { DropdownOption } from "./DropdownFilter";
import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export type FilterType = "button" | "dropdown";

export type BaseFilterItem<FilterValueType extends Key> = {
  title: string;
  value: FilterValueType;
  type: FilterType;
};

export type ButtonFilterItem<FilterValueType extends Key> =
  BaseFilterItem<FilterValueType> & {
    type: "button";
    status?: StatusCircleStatus;
    count: number;
  };

export type DropdownFilterItem<FilterValueType extends Key> =
  BaseFilterItem<FilterValueType> & {
    type: "dropdown";
    options: DropdownOption[];
    defaultOptionId: string;
  };

export type FilterItem<FilterValueType extends Key> =
  | ButtonFilterItem<FilterValueType>
  | DropdownFilterItem<FilterValueType>;
