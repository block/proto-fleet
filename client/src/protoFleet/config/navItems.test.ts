import { describe, expect, it } from "vitest";

import { isNavItemAllowedByPermissions, primaryNavItems, secondaryNavItems } from "./navItems";
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
  it("groups settings navigation by product area", () => {
    const labels = secondaryNavItems.map((item) => item.label);

    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/network",
        label: "Network",
        parent: "/settings",
        section: "Fleet",
      }),
    );
    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/preferences",
        label: "Preferences",
        parent: "/settings",
        section: "Account",
      }),
    );
    expect(labels.indexOf("Network")).toBeLessThan(labels.indexOf("Schedules"));
    expect(labels.indexOf("Schedules")).toBeLessThan(labels.indexOf("Security"));
    expect(labels.indexOf("Security")).toBeLessThan(labels.indexOf("Preferences"));
  });

  it("renames API key management to Integrations", () => {
    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/integrations",
        label: "Integrations",
        parent: "/settings",
        section: "Admin",
        requiredPermission: "apikey:manage",
      }),
    );
  });

  it("folds role management into the Team destination", () => {
    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/team",
        label: "Team",
        parent: "/settings",
        section: "Admin",
        requiredAnyPermission: ["user:read", "role:manage"],
      }),
    );
    expect(secondaryNavItems.some((item) => item.path === "/settings/roles")).toBe(false);
  });
});

describe("isNavItemAllowedByPermissions", () => {
  it("supports any-permission destinations", () => {
    const item = {
      path: "/settings/team",
      label: "Team",
      parent: "/settings",
      requiredAnyPermission: ["user:read", "role:manage"],
    };

    expect(isNavItemAllowedByPermissions(item, ["role:manage"])).toBe(true);
    expect(isNavItemAllowedByPermissions(item, ["user:read"])).toBe(true);
    expect(isNavItemAllowedByPermissions(item, ["apikey:manage"])).toBe(false);
  });
});
