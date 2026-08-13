import { describe, expect, it } from "vitest";
import { getTankPowerDialogCopy } from "./tankPowerDialogCopy";
import { variants } from "@/shared/components/Button";

describe("getTankPowerDialogCopy", () => {
  it("frames powering off as a destructive PDU cut (danger + critical)", () => {
    const copy = getTankPowerDialogCopy("Tank 1", false);
    expect(copy.title).toBe("Power off Tank 1?");
    expect(copy.confirmText).toBe("Power off");
    expect(copy.confirmVariant).toBe(variants.danger);
    expect(copy.iconIntent).toBe("critical");
    // The whole point of the dialog: spell out that this is the PDU, not a pause.
    expect(copy.body).toContain("PDU");
    expect(copy.body).toContain("not a mining pause");
    expect(copy.body).toContain("cuts line power");
    expect(copy.body).toContain("Tank 1");
  });

  it("frames powering on as a primary restore (primary + success)", () => {
    const copy = getTankPowerDialogCopy("Tank 6", true);
    expect(copy.title).toBe("Power on Tank 6?");
    expect(copy.confirmText).toBe("Power on");
    expect(copy.confirmVariant).toBe(variants.primary);
    expect(copy.iconIntent).toBe("success");
    expect(copy.body).toContain("PDU");
    expect(copy.body).toContain("not a mining pause");
    expect(copy.body).toContain("restores line power");
    expect(copy.body).toContain("Tank 6");
  });

  it("interpolates the tank label into both directions", () => {
    expect(getTankPowerDialogCopy("Tank 3", true).title).toBe("Power on Tank 3?");
    expect(getTankPowerDialogCopy("Tank 3", false).title).toBe("Power off Tank 3?");
  });
});
