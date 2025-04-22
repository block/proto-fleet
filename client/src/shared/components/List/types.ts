import { ReactNode } from "react";

export type ColConfig<ListItem, ItemKey> = {
  [K in keyof ListItem]?: {
    component?: (item: ListItem, selectedItems: ItemKey[]) => ReactNode;
    width: string;
  };
};

export type ColTitles<ColName extends string | number | symbol> = {
  [K in ColName]: string;
};

export type ListAction<ListItem> = {
  title: string;
  actionHandler: (item: ListItem) => void;
};
