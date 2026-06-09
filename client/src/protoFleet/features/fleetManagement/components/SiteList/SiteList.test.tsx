import { Fragment, type ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { type Site, SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

// Mock the popover stack used by FleetGroupActionsMenu so the menu
// items render directly into the test container (no portals + no
// trigger click required) — keeps these tests focused on list-level
// behavior.
vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
  popoverSizes: { small: "small" },
  default: ({ children, testId }: { children: ReactNode; testId?: string }) => (
    <div data-testid={testId}>{children}</div>
  ),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

// Stub the RPC + miner-command deps used by FleetGroupActionsMenu so
// the list-level tests focus on extras without firing real network
// calls.
vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: { listMinerStateSnapshots: vi.fn() },
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({
    stopMining: vi.fn(),
    startMining: vi.fn(),
    reboot: vi.fn(),
    downloadLogs: vi.fn(),
    streamCommandBatchUpdates: vi.fn(() => Promise.resolve()),
    getCommandBatchLogBundle: vi.fn(),
  }),
}));

vi.mock("../BulkActions/BulkActionConfirmDialog", () => ({ default: () => null }));

// eslint-disable-next-line import-x/order -- import must come after vi.mock calls
import SiteList from "./SiteList";

const makeSite = (id: number, name: string) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: BigInt(id), name }),
    buildingCount: 0n,
    deviceCount: 0n,
  });

const PathProbe = () => {
  const location = useLocation();
  return <span data-testid="probe-path">{location.pathname + location.search}</span>;
};

type EditSiteCallback = (site: Site) => void;

const renderList = ({ onEditSite }: { onEditSite?: EditSiteCallback } = {}) =>
  render(
    <MemoryRouter initialEntries={["/fleet/sites"]}>
      <Routes>
        <Route
          path="/fleet/sites"
          element={
            <>
              <SiteList sites={[makeSite(7, "North")]} onEditSite={onEditSite} />
              <PathProbe />
            </>
          }
        />
        <Route path="/sites/:id" element={<PathProbe />} />
        <Route path="/racks" element={<PathProbe />} />
        <Route path="/miners" element={<PathProbe />} />
        <Route path="/fleet/buildings" element={<PathProbe />} />
      </Routes>
    </MemoryRouter>,
  );

const trigger = () => screen.getByTestId("site-list-row-7-actions-trigger");

describe("SiteList row actions menu", () => {
  it("exposes the J10 Figma action set when the trigger is clicked", () => {
    renderList({ onEditSite: vi.fn() });
    fireEvent.click(trigger());
    for (const label of [
      "Manage power",
      "Update firmware",
      "Edit pool",
      "View site",
      "View buildings",
      "View racks",
      "View miners",
      "Edit site",
      "Add to group",
      "Manage security",
      "Unpair miners",
    ]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("View site navigates to the detail page", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View site"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/sites/7");
  });

  it("View miners deep-links to /miners with the site filter param", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View miners"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/miners?site=7");
  });

  it("View buildings deep-links to /fleet/buildings with the site filter param", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View buildings"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/buildings?site=7");
  });

  it("View racks deep-links to /racks with the site filter param", () => {
    renderList();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("View racks"));
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/racks?site=7");
  });

  it("Edit site forwards the row's Site to the host without navigating", () => {
    const onEditSite = vi.fn();
    renderList({ onEditSite });
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText("Edit site"));
    expect(onEditSite).toHaveBeenCalledTimes(1);
    expect(onEditSite.mock.calls[0][0].name).toBe("North");
    expect(screen.getByTestId("probe-path")).toHaveTextContent("/fleet/sites");
  });

  it("hides Edit site when the host does not supply a handler", () => {
    renderList();
    fireEvent.click(trigger());
    expect(screen.queryByText("Edit site")).not.toBeInTheDocument();
  });
});
