import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { useContainerOverview } from "../hooks/useContainerOverview";
import ContainerPage from "./ContainerPage";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const mocks = vi.hoisted(() => ({ load: vi.fn<typeof useContainerOverview>(), sync: vi.fn() }));
vi.mock("../hooks/useContainerOverview", () => ({ useContainerOverview: mocks.load }));
vi.mock("../components/ContainerPerformance", () => ({ default: () => <section aria-label="Performance" /> }));
vi.mock("../ModuleHistoryDialog", () => ({
  ModuleHistoryDialog: ({ item, onDismiss }: { item: { name: string }; onDismiss: () => void }) => (
    <div role="dialog" aria-label={item.name}>
      <button onClick={onDismiss}>Close history</button>
    </div>
  ),
}));
vi.mock("@/protoFleet/hooks/useSyncScopeToEntity", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSyncScopeToEntity: mocks.sync,
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => ({
    sites: [create(SiteWithCountsSchema, { site: { id: 2n, name: "North", slug: "north" } })],
  }),
}));
vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));

function Location() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  );
}
function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/containers/ct1"]}>
      <Routes>
        <Route path="/containers/:identifier" element={<ContainerPage />} />
        <Route path="*" element={<Location />} />
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
        id: 1n,
        deviceIdentifier: "ct1",
        name: "CT1 North",
        site: { id: 2n, label: "North" },
      }),
      tanks: [create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 81n }, position: 1, assetCount: 1n })],
      modules: [
        create(InventoryItemSchema, {
          ref: { kind: EntityKind.MODULE, id: 91n },
          ancestors: [{ kind: EntityKind.TANK, id: 81n }],
          name: "Tank 1 module 1",
          position: 1,
          state: "hashing",
        }),
      ],
    },
  });
});

describe("native container detail", () => {
  it("uses site breadcrumbs, Fleet scope and read-only collected module history", () => {
    renderPage();
    const breadcrumb = within(screen.getByTestId("container-overview-breadcrumb"));
    expect(breadcrumb.getByRole("link", { name: "North" })).toHaveAttribute("href", "/sites/2");
    expect(breadcrumb.queryByText("Buildings")).not.toBeInTheDocument();
    expect(mocks.sync).toHaveBeenCalledWith({ kind: "site", id: "2", slug: "north" });
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.getByText("No fan readings available.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Tank 1 module 1.*View module/ }));
    expect(screen.getByRole("dialog", { name: "Tank 1 module 1" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close history" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens the scoped asset list using the stable tank ID", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "View Tank 1" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/north/fleet/miners?tank=81");
  });

  it("uses building-style breadcrumbs and unassigned scope without a site", () => {
    const value = mocks.load("ct1");
    value.data!.container.site = undefined;
    renderPage();
    expect(screen.getByRole("link", { name: "Buildings" })).toHaveAttribute("href", "/fleet/buildings");
    fireEvent.click(screen.getByRole("button", { name: "View Tank 1" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/unassigned/fleet/miners?tank=81");
  });

  it("keeps the overview visible with a retry on a transient refresh failure", () => {
    const value = mocks.load("ct1");
    mocks.load.mockReturnValue({ ...value, error: "Network unavailable" });
    renderPage();
    expect(screen.getByRole("heading", { name: "CT1 North" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not refresh container");
    expect(screen.getByTestId("tank-module")).toHaveAttribute("data-module-state", "unknown");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(value.retry).toHaveBeenCalled();
  });

  it("offers a retry when the container cannot be loaded at all", () => {
    const retry = vi.fn();
    mocks.load.mockReturnValue({ loading: false, error: "Network unavailable", retry });
    renderPage();
    expect(screen.getByText("Couldn't load container")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Network unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalled();
  });

  it("shows loading, missing and empty inventory states", () => {
    const value = mocks.load("ct1");
    mocks.load.mockReturnValue({ loading: true, retry: value.retry });
    const { rerender } = renderPage();
    expect(screen.getByRole("status")).toHaveTextContent("Loading container");
    mocks.load.mockReturnValue({ loading: false, notFound: true, retry: value.retry });
    rerender(
      <MemoryRouter>
        <ContainerPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("Container not found")).toBeInTheDocument();
    mocks.load.mockReturnValue({ ...value, data: { ...value.data!, tanks: [], modules: [] } });
    rerender(
      <MemoryRouter>
        <ContainerPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("No tanks reported.")).toBeInTheDocument();
  });
});
