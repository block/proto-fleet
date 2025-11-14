import { ReactNode } from "react";

export type ColConfig<
  ListItem,
  ItemKey,
  ColKey extends string = keyof ListItem & string,
> = {
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
};
