import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SitePicker from "./SitePicker";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

vi.mock("@/protoFleet/store", () => ({
  useUsername: () => "alice",
}));

const localStorageStub = (() => {
  let store: Record<string, string> = {};
  return {
    reset: () => {
      store = {};
    },
    getItem: (key: string) => (key in store ? store[key] : null),
    setItem: (key: string, value: string) => {
      store[key] = value;
    },
    removeItem: (key: string) => {
      delete store[key];
    },
    clear: () => {
      store = {};
    },
  };
})();

beforeEach(() => {
  localStorageStub.reset();
  vi.stubGlobal("localStorage", localStorageStub);
});

const makeSiteWithCounts = (id: bigint, name: string) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id, name }),
    deviceCount: 0n,
    buildingCount: 0n,
    rackCount: 0n,
  });

describe("SitePicker", () => {
  it("renders a skeleton while sites are loading", () => {
    const { container } = render(<SitePicker sites={undefined} />);
    expect(container.querySelector("[data-testid='site-picker-trigger']")).toBeNull();
  });

  it("renders nothing when the org has zero sites", () => {
    const { container } = render(<SitePicker sites={[]} />);
    expect(container.querySelector("[data-testid='site-picker-trigger']")).toBeNull();
  });

  it("renders the current label and opens a list of options on click", () => {
    const sites = [makeSiteWithCounts(1n, "Austin"), makeSiteWithCounts(2n, "Boise")];
    render(<SitePicker sites={sites} />);

    const trigger = screen.getByTestId("site-picker-trigger");
    expect(trigger).toHaveTextContent("All sites");

    fireEvent.click(trigger);
    expect(screen.getByTestId("site-picker-option-all")).toHaveTextContent("All sites");
    expect(screen.getByTestId("site-picker-option-1")).toHaveTextContent("Austin");
    expect(screen.getByTestId("site-picker-option-2")).toHaveTextContent("Boise");
    expect(screen.getByTestId("site-picker-option-unassigned")).toHaveTextContent("Unassigned");
  });

  it("orders the site options by name ascending regardless of input order", () => {
    const sites = [makeSiteWithCounts(2n, "Boise"), makeSiteWithCounts(1n, "Austin")];
    render(<SitePicker sites={sites} />);
    fireEvent.click(screen.getByTestId("site-picker-trigger"));
    const modal = screen.getByTestId("site-picker-modal");
    const labels = Array.from(modal.querySelectorAll("button[data-testid^='site-picker-option-']")).map(
      (el) => el.textContent ?? "",
    );
    // "All sites" first, sites alphabetized, "Unassigned" last.
    expect(labels).toEqual(["All sites", "Austin", "Boise", "Unassigned"]);
  });

  it("persists the selection through localStorage", () => {
    const sites = [makeSiteWithCounts(1n, "Austin")];
    render(<SitePicker sites={sites} />);
    fireEvent.click(screen.getByTestId("site-picker-trigger"));
    fireEvent.click(screen.getByTestId("site-picker-option-1"));
    expect(localStorageStub.getItem("multiSite.activeSite:alice")).toContain('"id":"1"');
  });
});
