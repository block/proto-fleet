import { describe, expect, it } from "vitest";

import { getActivityIcon } from "./activityIcons";
import { formatLabel } from "./formatLabel";
import { Edit } from "@/shared/assets/icons";

describe("activity display helpers", () => {
  it("renders cohort updates with the edit icon and friendly label", () => {
    expect(getActivityIcon("cohort_updated")).toBe(Edit);
    expect(formatLabel("cohort_updated")).toBe("Cohort updated");
  });
});
