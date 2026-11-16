import { MemoryRouter, Route, Routes } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampNow } from "@bufbuild/protobuf/wkt";
import type { useContainerOverview } from "../hooks/useContainerOverview";
import ModulePage from "./ModulePage";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  InventoryItemSchema,
  ModuleHashboardsSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { useModuleHashboards } from "@/protoFleet/api/useModuleHashboards";

const mocks = vi.hoisted(() => ({
  load: vi.fn<typeof useContainerOverview>(),
  boards: vi.fn<typeof useModuleHashboards>(),
}));
vi.mock("../hooks/useContainerOverview", () => ({ useContainerOverview: mocks.load }));
vi.mock("@/protoFleet/api/useModuleHashboards", () => ({ useModuleHashboards: mocks.boards }));
vi.mock("@/protoFleet/api/SitesContext", () => ({ useSitesContext: () => ({ sites: [] }) }));
vi.mock("@/protoFleet/hooks/useSyncScopeToEntity", () => ({
  entityScopeTarget: () => ({ kind: "all" }),
  useSyncScopeToEntity: () => {},
}));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useTemperatureUnit: () => "C",
  usePermissions: () => [],
}));
vi.mock("../components/ModulePowerFeedCard", () => ({
  default: ({ moduleId }: { moduleId: bigint }) => (
    <article aria-label="Power feed">module {String(moduleId)} feed</article>
  ),
}));
vi.mock("../components/ModulePerformance", () => ({
  default: ({ moduleId }: { moduleId: bigint }) => (
    <section aria-label="Performance">module {String(moduleId)} history</section>
  ),
}));

function Page({ id = "91" }: { id?: string }) {
  return (
    <MemoryRouter initialEntries={[`/containers/ct1/tanks/81/modules/${id}`]}>
      <Routes>
        <Route path="/containers/:identifier/tanks/:tankId/modules/:moduleId" element={<ModulePage />} />
      </Routes>
    </MemoryRouter>
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  const ancestors = [{ kind: EntityKind.CONTAINER, id: 7n }];
  mocks.load.mockReturnValue({
    loading: false,
    retry: vi.fn(),
    data: {
      container: create(ContainerSchema, { id: 7n, name: "CT1 North", deviceIdentifier: "ct1" }),
      tanks: [create(InventoryItemSchema, { ref: { kind: EntityKind.TANK, id: 81n }, position: 1, ancestors })],
      modules: [
        create(InventoryItemSchema, {
          ref: { kind: EntityKind.MODULE, id: 91n },
          name: "CT1 North T1 M1",
          position: 1,
          deviceIdentifier: "ct1",
          ancestors: [...ancestors, { kind: EntityKind.TANK, id: 81n }],
          state: "error",
          observedAt: timestampNow(),
          hashrateThs: 600,
          powerKw: 18,
          efficiencyJth: 30,
          minTemperatureC: 60,
          maxTemperatureC: 60,
          moduleIssues: {
            observedAt: timestampNow(),
            complete: true,
            items: [
              { component: "psu", cause: "PSU_OVER_TEMPERATURE", severity: "warning", observedAt: timestampNow() },
            ],
          },
        }),
      ],
    },
  });
  mocks.boards.mockReturnValue({
    retry: vi.fn(),
    modules: {
      "91": {
        data: create(ModuleHashboardsSchema, {
          moduleId: 91n,
          slots: [
            {
              position: 1,
              occupancy: "occupied",
              observedAt: timestampNow(),
              board: {
                hashboardId: 11n,
                installationId: 21n,
                moduleId: 91n,
                position: 1,
                serial: "HB-ONE",
                state: "active",
                observedAt: timestampNow(),
                stateObservedAt: timestampNow(),
                hashrateHs: 200e12,
                temperatureC: 60,
                hashrailOk: true,
              },
            },
            { position: 2, occupancy: "empty", observedAt: timestampNow() },
            { position: 3, occupancy: "unknown", unavailableReason: "identity_conflict", observedAt: timestampNow() },
          ],
        }),
      },
    },
  });
});

it("renders Fleet metrics, the three slots, PSU causes and module history without ASIC UI", () => {
  render(<Page />);
  expect(screen.getByRole("heading", { level: 1, name: "CT1 North T1 M1" })).toBeInTheDocument();
  expect(screen.getByText("600.0 TH/s")).toBeInTheDocument();
  const hardware = screen.getByRole("region", { name: "Other hardware" });
  expect(within(hardware).getByRole("article", { name: "PSU 1" })).toBeInTheDocument();
  fireEvent.click(within(hardware).getByRole("button", { name: "PSU 1 info" }));
  expect(screen.getByRole("dialog", { name: "PSU 1 status" })).toHaveTextContent("PSU · Over temperature");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.queryByRole("navigation", { name: "Modules" })).not.toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Current issues" })).not.toBeInTheDocument();
  expect(screen.queryByText(/Last observed|Hashrail:|Reference clocks:/)).not.toBeInTheDocument();
  expect(screen.queryByText("PSU · Over temperature")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "CT1 North T1 M1 health: Needs attention" }));
  expect(screen.getByRole("dialog", { name: "CT1 North T1 M1 status" })).toHaveTextContent("PSU · Over temperature");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByText("Empty slot")).toBeInTheDocument();
  expect(screen.getByText("Board identity conflict")).toBeInTheDocument();
  expect(screen.queryByText("HB-ONE")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Board 1 info" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("HB-ONE");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.queryByText(/ASIC diagnostics/)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /View Board|Back to module/ })).not.toBeInTheDocument();
  expect(screen.getByRole("region", { name: "Performance" })).toHaveTextContent("module 91 history");
});
it("rejects replacement and retired module identities", () => {
  const data = mocks.load("ct1").data!;
  data.modules[0].ref!.id = 92n;
  const { unmount } = render(<Page />);
  expect(screen.getByRole("heading", { name: "Module unavailable" })).toBeInTheDocument();
  expect(screen.queryByText("HB-ONE")).not.toBeInTheDocument();
  unmount();
  data.modules[0].ref!.id = 91n;
  data.modules[0].state = "retired";
  render(<Page />);
  expect(screen.getByRole("heading", { name: "Module unavailable" })).toBeInTheDocument();
});
it("clears readings and actions on refresh failure, and clears identity on access loss", () => {
  const value = mocks.load("ct1");
  const { rerender } = render(<Page />);
  mocks.load.mockReturnValue({ ...value, error: "Network unavailable" });
  rerender(<Page />);
  expect(screen.queryByText("600.0 TH/s")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Actions for/ })).toBeInTheDocument();
  expect(within(screen.getByRole("region", { name: "Hashboards" })).queryByText("HB-ONE")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(value.retry).toHaveBeenCalled();
  mocks.load.mockReturnValue({ loading: false, error: "Access denied", retry: vi.fn() });
  rerender(<Page />);
  expect(screen.queryByRole("heading", { name: "CT1 North T1 M1" })).not.toBeInTheDocument();
});
it("keeps collected history available for stale controllers but hides current readings", () => {
  mocks.load("ct1").data!.container.stale = true;
  render(<Page />);
  expect(screen.getByText("Current readings are unavailable or stale.")).toBeInTheDocument();
  expect(screen.queryByText("600.0 TH/s")).not.toBeInTheDocument();
  expect(screen.getByRole("region", { name: "Performance" })).toHaveTextContent("module 91 history");
});

it("uses the Hashing status label when complete diagnostics report no issues", () => {
  const module = mocks.load("ct1").data!.modules[0];
  module.state = "active";
  module.moduleIssues!.items = [];
  render(<Page />);
  fireEvent.click(screen.getByRole("button", { name: /health: Hashing/ }));
  expect(screen.getByRole("dialog")).toHaveTextContent("No active component errors reported.");
});
it("opens unknown status details when issue evidence is unavailable", () => {
  mocks.load("ct1").data!.container.stale = true;
  render(<Page />);
  fireEvent.click(screen.getByRole("button", { name: /health: Status unknown/ }));
  expect(screen.getByRole("dialog")).toHaveTextContent("Issue report is partial.");
  expect(screen.queryByText("PSU · Over temperature")).not.toBeInTheDocument();
});

it.each([false, true])("presents isolation through Fleet health status (stale: %s)", (stale) => {
  const data = mocks.load("ct1").data!;
  data.container.stale = stale;
  data.modules[0].state = "inactive";
  data.modules[0].moduleIssues!.items = [];
  render(<Page />);
  const title = stale ? "Status unknown" : "Sleeping";
  const status = screen.getByRole("button", { name: `CT1 North T1 M1 health: ${title}` });
  expect(status.querySelector(stale ? ".bg-core-primary-50" : ".bg-intent-info-fill")).toBeInTheDocument();
  fireEvent.click(status);
  expect(screen.getByRole("dialog")).toHaveTextContent(title);
  expect(screen.queryByText(/isolated/i)).not.toBeInTheDocument();
});

it("shows a known offline state and offers server-checked actions without metric observations", () => {
  const module = mocks.load("ct1").data!.modules[0];
  module.state = "offline";
  module.observedAt = undefined;
  module.moduleIssues = undefined;
  render(<Page />);
  expect(screen.getByRole("button", { name: /health: Offline/ })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Actions for/ })).toBeInTheDocument();
  expect(screen.queryByText("600.0 TH/s")).not.toBeInTheDocument();
});

it("keeps board details open through a failed poll, clears data, and restores focus after closing", () => {
  const { rerender } = render(<Page />);
  const info = screen.getByRole("button", { name: "Board 1 info" });
  info.focus();
  fireEvent.click(info);
  expect(screen.getByRole("dialog")).toHaveTextContent("HB-ONE");
  mocks.boards.mockReturnValue({ retry: vi.fn(), modules: { "91": { error: "Offline" } } });
  rerender(<Page />);
  expect(screen.getByRole("dialog")).toHaveTextContent("Status unknown");
  expect(screen.getByRole("dialog")).not.toHaveTextContent("HB-ONE");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.getByRole("button", { name: "Board 1 info" })).toHaveFocus();
});
