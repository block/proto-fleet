import { describe, expect, it } from "vitest";

import {
  getBulkRenameFailureMessage,
  getBulkRenameLoadingMessage,
  getBulkRenameRequestFailureMessage,
  getBulkRenameSuccessMessage,
} from "./bulkRenameToastMessages";

describe("bulkRenameToastMessages", () => {
  it("builds loading messages for single and bulk renames", () => {
    expect(getBulkRenameLoadingMessage(1)).toBe("Renaming miner");
    expect(getBulkRenameLoadingMessage(3)).toBe("Renaming miners");
  });

  it("builds success messages for renamed-only, unchanged-only, and mixed outcomes", () => {
    expect(getBulkRenameSuccessMessage(2, 0)).toBe("Renamed 2 miners");
    expect(getBulkRenameSuccessMessage(0, 1)).toBe("1 miner unchanged");
    expect(getBulkRenameSuccessMessage(4, 2)).toBe("Renamed 4 miners; 2 miners unchanged");
  });

  it("builds failure messages for partial and full failures", () => {
    expect(getBulkRenameFailureMessage(1)).toBe("Failed to rename 1 miner");
    expect(getBulkRenameFailureMessage(5)).toBe("Failed to rename 5 miners");
  });

  it("builds request failure messages for single and bulk renames", () => {
    expect(getBulkRenameRequestFailureMessage(1)).toBe("Failed to rename miner");
    expect(getBulkRenameRequestFailureMessage(2)).toBe("Failed to rename miners");
  });
});
