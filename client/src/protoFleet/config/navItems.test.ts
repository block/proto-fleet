import { describe, expect, it } from "vitest";

import { getSettingsLandingPath, isNavItemAllowedByPermissions, primaryNavItems, secondaryNavItems } from "./navItems";
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

  it("gates Fleet on the permissions that actually make a tab reachable", () => {
    const fleet = primaryNavItems.find((item) => item.label === "Fleet");

    expect(fleet?.requiredPermission).toBeUndefined();
    expect(fleet?.requiredAnyPermission).toEqual(["rack:read", "site:read", ["miner:read", "fleet:read"]]);

    // Rack- and site-only readers can reach Fleet tabs (racks / sites), so the
    // nav entry stays visible for them.
    expect(isNavItemAllowedByPermissions(fleet!, ["rack:read"])).toBe(true);
    expect(isNavItemAllowedByPermissions(fleet!, ["site:read"])).toBe(true);
    // The miners tab needs miner:read AND fleet:read together, so only the pair
    // makes the nav entry visible via the miners path.
    expect(isNavItemAllowedByPermissions(fleet!, ["miner:read", "fleet:read"])).toBe(true);
    // Neither half alone reaches a tab (read-pairing does not force fleet:read
    // onto miner:read), so neither may advertise the page.
    expect(isNavItemAllowedByPermissions(fleet!, ["miner:read"])).toBe(false);
    expect(isNavItemAllowedByPermissions(fleet!, ["fleet:read"])).toBe(false);
    expect(isNavItemAllowedByPermissions(fleet!, ["activity:read"])).toBe(false);
  });

  it("gates Groups on rack:read to match the server-side device-set read gate", () => {
    const groups = primaryNavItems.find((item) => item.label === "Groups");

    expect(groups?.requiredPermission).toBe("rack:read");
    expect(groups?.requiredAnyPermission).toBeUndefined();
    expect(isNavItemAllowedByPermissions(groups!, ["rack:read"])).toBe(true);
    expect(isNavItemAllowedByPermissions(groups!, ["fleet:read"])).toBe(false);
  });

  it("leaves Home ungated as the universal landing", () => {
    const home = primaryNavItems.find((item) => item.label === "Home");

    expect(home?.requiredPermission).toBeUndefined();
    expect(home?.requiredAnyPermission).toBeUndefined();
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
        requiredPermission: "fleet:read",
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
    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/firmware",
        label: "Firmware",
        parent: "/settings",
        section: "Fleet",
        requiredPermission: "miner:firmware_update",
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

  it("gives agent configuration its own Admin destination", () => {
    expect(secondaryNavItems).toContainEqual(
      expect.objectContaining({
        path: "/settings/agents",
        label: "Agents",
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

describe("getSettingsLandingPath", () => {
  it("uses Network for fleet readers and Preferences as the safe fallback", () => {
    expect(getSettingsLandingPath(["fleet:read"])).toBe("/settings/network");
    expect(getSettingsLandingPath(["role:manage"])).toBe("/settings/preferences");
    expect(getSettingsLandingPath([])).toBe("/settings/preferences");
  });
});
