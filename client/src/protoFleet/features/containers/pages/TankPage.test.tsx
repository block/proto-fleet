import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { useContainerOverview } from "../hooks/useContainerOverview";
import TankPage from "./TankPage";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const mocks = vi.hoisted(() => ({ load: vi.fn<typeof useContainerOverview>(), sync: vi.fn() }));
vi.mock("../hooks/useContainerOverview", () => ({ useContainerOverview: mocks.load }));
vi.mock("../components/TankPerformance", () => ({ default: () => <section aria-label="Performance" /> }));
vi.mock("@/protoFleet/hooks/useSyncScopeToEntity", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSyncScopeToEntity: mocks.sync,
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => ({
    sites: [create(SiteWithCountsSchema, { site: { id: 2n, name: "North", slug: "north" } })],
  }),
}));
vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C", usePermissions: () => [] }));

function Location() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  );
}
function renderPage(path = "/containers/remote/tanks/81") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Location />
      <Routes>
        <Route path="/containers/:identifier/tanks/:tankId" element={<TankPage />} />
        <Route path="/containers/:identifier/tanks/:tankId/modules/:moduleId" element={<p>Module destination</p>} />
        <Route path="*" element={<p>Destination</p>} />
      </Routes>
    </MemoryRouter>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.load.mockReturnValue({
    loading: false,
    retry: vi.fn(),
    data: {
      container: create(ContainerSchema, {
        id: 7n,
        name: "CT1 North",
        deviceIdentifier: "remote",
        site: { id: 2n, label: "North" },
      }),
      tanks: [
        create(InventoryItemSchema, {
          ref: { kind: EntityKind.TANK, id: 81n },
          position: 1,
          assetCount: 1n,
          ancestors: [{ kind: EntityKind.CONTAINER, id: 7n }],
        }),
      ],
      modules: [
        create(InventoryItemSchema, {
          ref: { kind: EntityKind.MODULE, id: 91n },
          name: "Tank 1 module 1",
          position: 1,
          state: "isolated",
          ancestors: [
            { kind: EntityKind.CONTAINER, id: 7n },
            { kind: EntityKind.TANK, id: 81n },
          ],
        }),
      ],
    },
  });
});

describe("native tank page", () => {
  it("shows unknown module status even while the controller remains fresh", () => {
    const data = mocks.load("remote").data!;
    data.container.stale = false;
    // Inventory expires module state independently of the controller observation.
    data.modules[0].state = "unknown";
    renderPage();
    expect(screen.getByText("1 module unknown")).toBeInTheDocument();
    expect(screen.getByTestId("tank-module").querySelector('[data-tank-status="unknown"]')).toBeInTheDocument();
    expect(screen.queryByText("Readings unavailable or stale.")).not.toBeInTheDocument();
  });

  it("shows tank identity, site scope, collected states without redundant copy or device actions", () => {
    renderPage();
    expect(screen.getByRole("heading", { name: "CT1 North Tank 1" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Buildings" })).toHaveAttribute("href", "/north/fleet/buildings");
    expect(screen.getByRole("link", { name: "CT1 North" })).toHaveAttribute("href", "/containers/remote");
    expect(mocks.sync).toHaveBeenCalledWith({ kind: "site", id: "2", slug: "north" });
    expect(screen.getByTestId("tank-module")).toHaveAttribute("data-module-state", "isolated");
    expect(screen.queryByRole("heading", { name: "Cooling and equipment" })).not.toBeInTheDocument();
    expect(screen.queryByText("North · CT1 North")).not.toBeInTheDocument();
    expect(screen.queryByText(/modules reported/)).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByText("Reboot")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View modules" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/north/fleet/miners?tank=81");
  });

  it("opens native module detail at a deep link", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: /Tank 1 module 1.*View module/ }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/remote/tanks/81/modules/91");
    expect(screen.getByText("Module destination")).toBeInTheDocument();
  });

  it("does not render a colliding rack or tank belonging to another container", () => {
    mocks.load("remote").data!.tanks[0].ancestors[0].id = 8n;
    renderPage();
    expect(screen.getByRole("heading", { name: "Tank not found" })).toBeInTheDocument();
    expect(screen.queryByTestId("tank-module")).not.toBeInTheDocument();
  });

  it("preserves topology as unknown on refresh failure and offers retry", () => {
    const value = mocks.load("remote");
    mocks.load.mockReturnValue({ ...value, error: "Offline" });
    renderPage();
    expect(screen.getByTestId("tank-module")).toHaveAttribute("data-module-state", "unknown");
    expect(screen.getByRole("alert")).toHaveTextContent("Could not refresh tank");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(value.retry).toHaveBeenCalled();
  });

  it("keeps unassigned tanks scoped when opening the inventory list", () => {
    mocks.load("remote").data!.container.site = undefined;
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "View modules" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/unassigned/fleet/miners?tank=81");
  });

  it("shows a load failure without retained readings after access is denied", () => {
    mocks.load.mockReturnValue({ loading: false, error: "Access denied", retry: vi.fn() });
    renderPage();
    expect(screen.getByRole("alert")).toHaveTextContent("Access denied");
    expect(screen.queryByTestId("tank-module")).not.toBeInTheDocument();
  });
});
