import { ReactNode } from "react";

export type SortDirection = "asc" | "desc";

/** Sort direction constants */
export const SORT_ASC: SortDirection = "asc";
export const SORT_DESC: SortDirection = "desc";

export type ColConfig<ListItem, ItemKey, ColKey extends string = keyof ListItem & string> = {
  [K in ColKey]?: {
    component?: (item: ListItem, selectedItems: ItemKey[]) => ReactNode;
    width: string;
  };
};

export type ColTitles<ColKey extends string> = {
  [K in ColKey]: string;
};

export type ListAction<ListItem> = {
  title: string;
  actionHandler: (item: ListItem) => void;
  icon?: ReactNode;
  variant?: "default" | "destructive";
};
