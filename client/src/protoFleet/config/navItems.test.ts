import { describe, expect, it } from "vitest";

import { primaryNavItems, secondaryNavItems } from "./navItems";
import { LightningAlt } from "@/shared/assets/icons";

describe("primaryNavItems", () => {
  it("shows Energy above Activity with the electric icon", () => {
    const labels = primaryNavItems.map((item) => item.label);
    const energyItem = primaryNavItems.find((item) => item.label === "Energy");

    expect(energyItem).toMatchObject({
      path: "/energy",
      icon: LightningAlt,
      requiredPermission: "curtailment:read",
    });
    expect(labels.indexOf("Energy")).toBe(labels.indexOf("Activity") - 1);
  });
});

describe("secondaryNavItems", () => {
  it("shows Curtailment above API Keys in settings navigation", () => {
    const labels = secondaryNavItems.map((item) => item.label);

    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/curtailment",
        label: "Curtailment",
        parent: "/settings",
        requiredPermission: "curtailment:manage",
      }),
    );
    expect(labels.indexOf("Curtailment")).toBe(labels.indexOf("API Keys") - 1);
  });
});
