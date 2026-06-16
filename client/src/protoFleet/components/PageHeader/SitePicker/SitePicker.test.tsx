// SitePicker now persists its selection via the Zustand UI slice (org-wide,
// not per-username localStorage). The persistence contract is covered by
// useActiveSite.test.ts; these tests focus on render shapes, modal options,
// the new error/retry affordance, and the useNavigate handoff to /settings/sites.
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SitePicker from "./SitePicker";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mockNavigate = vi.fn();
vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

beforeEach(() => {
  mockNavigate.mockReset();
  useFleetStore.setState((state) => {
    state.ui.activeSite = DEFAULT_ACTIVE_SITE;
  });
});

const makeSiteWithCounts = (id: bigint, name: string) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id, name }),
    deviceCount: 0n,
    buildingCount: 0n,
    rackCount: 0n,
  });

const renderPicker = (props: Parameters<typeof SitePicker>[0]) =>
  render(
    <MemoryRouter>
      <SitePicker {...props} />
    </MemoryRouter>,
  );

describe("SitePicker", () => {
  it("renders a skeleton while sites are loading", () => {
    const { container } = renderPicker({ sites: undefined });
    expect(container.querySelector("[data-testid='site-picker-trigger']")).toBeNull();
  });

  it("renders nothing when the org has zero sites and no error", () => {
    const { container } = renderPicker({ sites: [] });
    expect(container.querySelector("[data-testid='site-picker-trigger']")).toBeNull();
    expect(container.querySelector("[data-testid='site-picker-error']")).toBeNull();
  });

  it("renders the retry affordance when ListSites failed", () => {
    const onRetry = vi.fn();
    renderPicker({ sites: [], error: "network down", onRetry });
    expect(screen.getByTestId("site-picker-error")).toHaveTextContent("Sites unavailable");
    fireEvent.click(screen.getByTestId("site-picker-retry"));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("renders the current label and opens a list of options on click", () => {
    const sites = [makeSiteWithCounts(1n, "Austin"), makeSiteWithCounts(2n, "Boise")];
    renderPicker({ sites });

    const trigger = screen.getByTestId("site-picker-trigger");
    const label = screen.getByText("All sites");
    expect(trigger).toHaveClass("max-w-full", "min-w-0");
    expect(label).toHaveClass("min-w-0", "truncate");
    expect(trigger).toHaveTextContent("All sites");

    fireEvent.click(trigger);
    expect(screen.getByTestId("site-picker-option-all")).toHaveTextContent("All sites");
    expect(screen.getByTestId("site-picker-option-1")).toHaveTextContent("Austin");
    expect(screen.getByTestId("site-picker-option-2")).toHaveTextContent("Boise");
    expect(screen.getByTestId("site-picker-option-unassigned")).toHaveTextContent("Unassigned");
  });

  it("orders the site options by name ascending regardless of input order", () => {
    const sites = [makeSiteWithCounts(2n, "Boise"), makeSiteWithCounts(1n, "Austin")];
    renderPicker({ sites });
    fireEvent.click(screen.getByTestId("site-picker-trigger"));
    const modal = screen.getByTestId("site-picker-modal");
    const labels = Array.from(modal.querySelectorAll("button[data-testid^='site-picker-option-']")).map(
      (el) => el.textContent ?? "",
    );
    // "All sites" first, sites alphabetized, "Unassigned" last.
    expect(labels).toEqual(["All sites", "Austin", "Boise", "Unassigned"]);
  });

  it("persists the selection through the Zustand UI slice", () => {
    const sites = [makeSiteWithCounts(1n, "Austin")];
    renderPicker({ sites });
    fireEvent.click(screen.getByTestId("site-picker-trigger"));
    fireEvent.click(screen.getByTestId("site-picker-option-1"));
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "1" });
  });

  it("navigates to /settings/sites via react-router when Manage sites is clicked", () => {
    const sites = [makeSiteWithCounts(1n, "Austin")];
    renderPicker({ sites });
    fireEvent.click(screen.getByTestId("site-picker-trigger"));
    fireEvent.click(screen.getByTestId("site-picker-manage-sites"));
    expect(mockNavigate).toHaveBeenCalledWith("/settings/sites");
  });
});
