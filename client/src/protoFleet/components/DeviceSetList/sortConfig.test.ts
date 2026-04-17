import { describe, expect, it } from "vitest";

import { getNextSortFromSelection } from "./sortConfig";
import { SORT_ASC, SORT_DESC } from "@/shared/components/List/types";

type SortState = Parameters<typeof getNextSortFromSelection>[1];

describe("getNextSortFromSelection", () => {
  it("uses ascending when selecting miners from the dropdown", () => {
    const currentSort: SortState = {
      field: "name",
      direction: SORT_ASC,
    };

    expect(getNextSortFromSelection(["miners"], currentSort)).toEqual({
      field: "miners",
      direction: SORT_ASC,
    });
  });

  it("uses descending when selecting issues from the dropdown", () => {
    const currentSort: SortState = {
      field: "name",
      direction: SORT_ASC,
    };

    expect(getNextSortFromSelection(["issues"], currentSort)).toEqual({
      field: "issues",
      direction: SORT_DESC,
    });
  });

  it("toggles the current sort when the selection is invalid", () => {
    const currentSort: SortState = {
      field: "name",
      direction: SORT_ASC,
    };

    expect(getNextSortFromSelection(["not-a-real-column"], currentSort)).toEqual({
      field: "name",
      direction: SORT_DESC,
    });
  });
});
