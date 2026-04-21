import { ReactNode } from "react";

export type SortDirection = "asc" | "desc";

/** Sort direction constants */
export const SORT_ASC: SortDirection = "asc";
export const SORT_DESC: SortDirection = "desc";

export type ColConfig<ListItem, ItemKey, ColKey extends string = keyof ListItem & string> = {
  [K in ColKey]?: {
    component?: (item: ListItem, selectedItems: ItemKey[]) => ReactNode;
    width: string;
    allowWrap?: boolean;
  };
};

export type ColTitles<ColKey extends string> = {
  [K in ColKey]: string;
};

export type ListActionValue<ListItem, Value> = Value | ((item: ListItem) => Value);

export type ListAction<ListItem> = {
  title: ListActionValue<ListItem, string>;
  actionHandler: (item: ListItem) => void;
  icon?: ListActionValue<ListItem, ReactNode>;
  variant?: ListActionValue<ListItem, "default" | "destructive">;
  disabled?: ListActionValue<ListItem, boolean>;
  hidden?: ListActionValue<ListItem, boolean>;
  showDividerAfter?: ListActionValue<ListItem, boolean>;
};

export const resolveListActionValue = <ListItem, Value>(
  value: ListActionValue<ListItem, Value> | undefined,
  item: ListItem,
): Value | undefined => {
  if (typeof value === "function") {
    return (value as (item: ListItem) => Value)(item);
  }

  return value;
};
