import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampNow } from "@bufbuild/protobuf/wkt";
import type { useContainerOverview } from "../hooks/useContainerOverview";
import type { useEquipmentCommand } from "../hooks/useEquipmentCommand";
import ContainerPage from "./ContainerPage";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const mocks = vi.hoisted(() => ({
  load: vi.fn<typeof useContainerOverview>(),
  sync: vi.fn(),
  command: vi.fn<typeof useEquipmentCommand>(),
}));
vi.mock("../hooks/useEquipmentCommand", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useEquipmentCommand: mocks.command,
}));
vi.mock("../hooks/useContainerOverview", () => ({ useContainerOverview: mocks.load }));
vi.mock("../components/ContainerPerformance", () => ({ default: () => <section aria-label="Performance" /> }));
vi.mock("@/protoFleet/hooks/useSyncScopeToEntity", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useSyncScopeToEntity: mocks.sync,
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => ({
    sites: [create(SiteWithCountsSchema, { site: { id: 2n, name: "North", slug: "north" } })],
  }),
}));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => "C",
  usePermissions: () => [],
}));

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
  mocks.command.mockReturnValue({
    submit: vi.fn(),
    busy: false,
    pendingName: undefined,
    message: undefined,
    failed: false,
  });
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
  it("shows activity beside the affected control until readback, then clears it or reports failure", () => {
    const container = mocks.load("ct1").data!.container;
    Object.assign(
      container,
      create(ContainerSchema, {
        name: "CT1 North",
        canControlEquipment: true,
        controls: {
          supported: true,
          observedAt: timestampNow(),
          controls: [
            { name: "dry_cooler", enabled: true },
            { name: "coolant_pump", enabled: true },
          ],
        },
      }),
    );
    const command = {
      submit: vi.fn(),
      busy: true,
      pendingName: "dry_cooler",
      message: "Equipment command pending…",
      failed: false,
    };
    mocks.command.mockReturnValue(command);
    const { rerender } = renderPage();
    const update = () =>
      rerender(
        <MemoryRouter>
          <ContainerPage />
        </MemoryRouter>,
      );
    const cooler = () => within(screen.getByTestId("container-control-dry_cooler"));
    expect(cooler().getByRole("status", { name: "Updating Dry cooler" })).toBeInTheDocument();
    expect(within(screen.getByTestId("container-control-coolant_pump")).queryByRole("status")).not.toBeInTheDocument();
    expect(cooler().getByRole("checkbox")).toBeDisabled();
    expect(screen.queryByText(command.message)).not.toBeInTheDocument();
    command.message = "Write acknowledged; waiting for controller readback…";
    update();
    expect(cooler().getByRole("status", { name: "Updating Dry cooler" })).toBeInTheDocument();
    expect(screen.queryByText(command.message)).not.toBeInTheDocument();
    mocks.command.mockReturnValue({
      ...command,
      busy: false,
      pendingName: undefined,
      message: "Equipment state confirmed by the controller.",
    });
    update();
    expect(cooler().queryByRole("status")).not.toBeInTheDocument();
    expect(cooler().getByRole("checkbox")).toBeEnabled();
    expect(screen.queryByText("Equipment state confirmed by the controller.")).not.toBeInTheDocument();
    mocks.command.mockReturnValue({
      ...command,
      busy: false,
      pendingName: undefined,
      failed: true,
      message: "Controller rejected the command.",
    });
    update();
    expect(screen.getByRole("alert")).toHaveTextContent("Controller rejected the command.");
    expect(cooler().queryByRole("status")).not.toBeInTheDocument();
  });

  it("uses site breadcrumbs, Fleet scope and the native module destination", () => {
    renderPage();
    const breadcrumb = within(screen.getByTestId("container-overview-breadcrumb"));
    expect(breadcrumb.getByRole("link", { name: "North" })).toHaveAttribute("href", "/sites/2");
    expect(breadcrumb.queryByText("Buildings")).not.toBeInTheDocument();
    expect(mocks.sync).toHaveBeenCalledWith({ kind: "site", id: "2", slug: "north" });
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.getByText("No fan readings available.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Tank 1 module 1.*View module/ }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1/tanks/81/modules/91");
  });

  it("opens the native tank detail using the stable tank ID", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "View Tank 1" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1/tanks/81");
  });

  it("uses building-style breadcrumbs and unassigned scope without a site", () => {
    const value = mocks.load("ct1");
    value.data!.container.site = undefined;
    renderPage();
    expect(screen.getByRole("link", { name: "Buildings" })).toHaveAttribute("href", "/fleet/buildings");
    fireEvent.click(screen.getByRole("button", { name: "View Tank 1" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/containers/ct1/tanks/81");
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

it.each([1, 6, 288])("shows %i unlocated hashboards below the container title", (count) => {
  const container = mocks.load("ct1").data!.container;
  container.unlocatedHashboards = count;
  renderPage();
  const notice = screen.getByText(
    count === 1
      ? "1 hashboard has an unknown location and is excluded from tank and module telemetry."
      : `${count} hashboards have unknown locations and are excluded from tank and module telemetry.`,
  );
  expect(notice).toHaveClass("text-text-critical");
  expect(notice).toHaveAttribute("role", "status");
  expect(notice.parentElement).toContainElement(screen.getByRole("heading", { name: "CT1 North", level: 1 }));
});

it.each(["zero", "unknown", "stale", "refresh error"])("hides the unlocated notice for %s evidence", (state) => {
  const current = mocks.load("ct1");
  current.data!.container.unlocatedHashboards = state === "unknown" ? undefined : state === "zero" ? 0 : 6;
  current.data!.container.stale = state === "stale";
  if (state === "refresh error") current.error = "Connection unavailable";
  renderPage();
  expect(screen.queryByText(/unknown location/)).not.toBeInTheDocument();
});

it("removes headline hashrate coverage without treating nonreporting boards as unlocated", () => {
  const container = mocks.load("ct1").data!.container;
  Object.assign(
    container,
    create(ContainerSchema, {
      name: "CT1 North",
      hashrateHs: 50e15,
      coverage: { hashrate: { reporting: 272, expected: 288, topologyComplete: true } },
    }),
  );
  renderPage();
  expect(screen.getByText("Hashrate")).toBeInTheDocument();
  expect(screen.queryByText("272/288 boards reporting")).not.toBeInTheDocument();
  expect(screen.queryByText(/unknown location/)).not.toBeInTheDocument();
});
