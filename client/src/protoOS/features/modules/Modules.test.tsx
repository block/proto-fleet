import { MemoryRouter, Outlet, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import ModuleHashboardOverlay from "./ModuleHashboardOverlay";
import Modules from "./Modules";
import { CONTAINER_MODULE_LAYOUT } from "@/protoOS/features/diagnostic/hashboardLayout/layouts";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asEntityKey } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  minerRoot: "",
  error: undefined as string | undefined,
  command: vi.fn(),
  telemetry: vi.fn(),
}));
const adapter = { runHashboardAction: mocks.command, getCurrentTelemetry: mocks.telemetry };
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ minerRoot: mocks.minerRoot, adapter }),
}));
vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry:
    () =>
    async ({
      request,
      onSuccess,
    }: {
      request: (params: unknown) => Promise<unknown>;
      onSuccess: (result: unknown) => Promise<void>;
    }) =>
      onSuccess(await request({})),
}));
vi.mock("@/protoOS/api/hooks/useTelemetry", () => ({ useTelemetry: () => ({ error: mocks.error }) }));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({
  useHashboardStatus: () => ({ error: undefined, pending: false }),
}));
vi.mock("@/protoOS/features/diagnostic/hashboardLayout", async (original) => ({
  ...(await original<object>()),
  useHashboardLayout: () => CONTAINER_MODULE_LAYOUT,
}));

const Location = () => {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <span data-testid="location">
        {location.pathname}
        {location.search}
      </span>
      <button onClick={() => navigate(-1)}>History back</button>
    </>
  );
};
const showPage = (path = "/modules") =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Location />
      <Routes>
        <Route path={`${mocks.minerRoot}/modules`} element={<Modules />}>
          <Route path=":tankId/:moduleId" element={<Outlet />}>
            <Route path=":serial" element={<ModuleHashboardOverlay />} />
          </Route>
        </Route>
      </Routes>
    </MemoryRouter>,
  );
let fixture: ReturnType<typeof tanksFixture>;

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 100, 40));
  mocks.command.mockReset().mockResolvedValue({ accepted: 3, failures: [] });
  mocks.telemetry.mockReset().mockResolvedValue({});
  mocks.minerRoot = "";
  mocks.error = undefined;
  fixture = tanksFixture(2, 12);
  const store = useMinerStore.getState();
  store.resetDeviceData();
  store.hardware.replaceHardware(fixture.hardware);
  store.telemetry.mergeTelemetry(fixture.telemetry);
});

describe("Modules", () => {
  test("selects/deselects visible modules, and row actions ignore the bulk selection", async () => {
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all modules" }));
    await waitFor(() => expect(screen.getByText("24 modules selected")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("checkbox", { name: "Select T1-P1" }));
    expect(screen.getByRole("checkbox", { name: "Select all modules" })).toHaveAttribute("aria-checked", "mixed");
    await waitFor(() => expect(screen.getByText("23 modules selected")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Actions for T1-P1" }));
    fireEvent.click(await screen.findByTestId("module-action-stop"));
    const dialog = await screen.findByRole("dialog", { name: "Stop mining" });
    expect(dialog).toHaveTextContent("3 hashboards in 1 module");
    fireEvent.click(within(dialog).getAllByRole("button", { name: "Stop mining" })[0]);
    await waitFor(() => expect(mocks.command).toHaveBeenCalledTimes(1));
    expect(mocks.command.mock.calls[0][1]).toEqual(["T1-M1-HB1", "T1-M1-HB2", "T1-M1-HB3"]);
    await waitFor(() => expect(within(dialog).getAllByRole("button", { name: "Done" })[0]).toBeEnabled());
    fireEvent.click(within(dialog).getAllByRole("button", { name: "Done" })[0]);
    fireEvent.click(screen.getByRole("button", { name: "Clear selection" }));
    await waitFor(() => expect(screen.queryByTestId("action-bar")).not.toBeInTheDocument());
  });

  test("bulk menus use all and only the filtered selection", async () => {
    showPage("/modules?status=attention");
    const selectedRows = [...fixture.topology.modules.values()].filter((module) =>
      module.hashboardIds.some((id) => fixture.telemetry.hashboards.get(id)?.health === "fault"),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all modules" }));
    fireEvent.click(await screen.findByRole("button", { name: "Actions for selected modules" }));
    fireEvent.click(await screen.findByTestId("module-action-identify"));
    const dialog = await screen.findByRole("dialog", { name: "Blink LEDs" });
    fireEvent.click(within(dialog).getAllByRole("button", { name: "Blink LEDs" })[0]);
    await waitFor(() => expect(mocks.command).toHaveBeenCalledTimes(1));
    expect(mocks.command.mock.calls[0][1]).toEqual(
      selectedRows
        .flatMap((module) => module.hashboardIds.map((id) => fixture.topology.hashboards.get(id)!.identity.value))
        .sort(),
    );
  });

  test("prunes select-all targets when filters narrow the visible modules", async () => {
    showPage();
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all modules" }));
    await screen.findByText("24 modules selected");
    fireEvent.click(screen.getByTestId("filter-dropdown-Status"));
    fireEvent.click(await screen.findByTestId("filter-option-attention"));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByText("2 of 24 modules");
    await screen.findByText("2 modules selected");
    fireEvent.click(screen.getByRole("button", { name: "Actions for selected modules" }));
    fireEvent.click(await screen.findByTestId("module-action-identify"));
    const dialog = await screen.findByRole("dialog", { name: "Blink LEDs" });
    fireEvent.click(within(dialog).getAllByRole("button", { name: "Blink LEDs" })[0]);
    await waitFor(() => expect(mocks.command).toHaveBeenCalledTimes(1));
    const expected = [...fixture.topology.modules.values()]
      .filter((module) => module.hashboardIds.some((id) => fixture.telemetry.hashboards.get(id)?.health === "fault"))
      .flatMap((module) => module.hashboardIds.map((id) => fixture.topology.hashboards.get(id)!.identity.value))
      .sort();
    expect(mocks.command.mock.calls[0][1]).toEqual(expected);
  });

  test("applies status filters through the shared controls and restores them with Back", async () => {
    showPage();
    fireEvent.click(screen.getByTestId("filter-dropdown-Status"));
    fireEvent.click(await screen.findByTestId("filter-option-attention"));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(screen.getByText("2 of 24 modules")).toBeInTheDocument());
    expect(screen.queryByText("T1-P1")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "History back" }));
    await waitFor(() => expect(screen.getByText("24 modules")).toBeInTheDocument());
  });
  test("renders live position names, counts, health and numeric name sorting", () => {
    showPage();
    expect(screen.getByRole("heading", { name: "Modules" })).toBeInTheDocument();
    expect(screen.getByText("24 modules")).toBeInTheDocument();
    expect(screen.getAllByRole("row")[2]).toHaveTextContent("T1-P2");
    expect(screen.getByText("T2-P1")).toBeInTheDocument();
    const board = [...fixture.telemetry.hashboards.values()][0];
    fixture.telemetry.hashboards.set(board.hashboardId, { ...board, health: "fault", reportedIssueCount: 2 });
    act(() => useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry));
    expect(screen.getByText("T1-P1").closest("tr")).toHaveTextContent("Needs attention");
  });

  test("displays module metrics in preferred units and preserves missing readings", () => {
    const [first, second] = [...fixture.topology.modules.values()];
    fixture.telemetry.rollupsByEntity.set(asEntityKey(first.id), {
      temperature: { latest: { value: 65, units: "°C" } },
      power: { latest: { value: 4500, units: "W" } },
      efficiency: { latest: { value: 15, units: "J/TH" } },
    });
    fixture.telemetry.rollupsByEntity.set(asEntityKey(second.id), {
      temperature: { latest: { value: null, units: "°C" } },
      power: { latest: { value: null, units: "W" } },
      efficiency: { latest: { value: null, units: "J/TH" } },
    });
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    useMinerStore.getState().ui.setTemperatureUnit("C");
    showPage();
    const row = screen.getByText("T1-P1").closest("tr")!;
    expect(row).toHaveTextContent("65.0 °C");
    expect(row).toHaveTextContent("4.5 kW");
    expect(row).toHaveTextContent("15.0 J/TH");
    expect(screen.queryByRole("columnheader", { name: "Run-in" })).not.toBeInTheDocument();
    const missingRow = screen.getByText("T1-P2").closest("tr")!;
    for (const metric of ["temperature", "power", "efficiency"]) {
      expect(within(missingRow).getByTestId(metric)).toHaveTextContent("—");
    }
    act(() => useMinerStore.getState().ui.setTemperatureUnit("F"));
    expect(row).toHaveTextContent("149.0 °F");
    act(() => useMinerStore.getState().ui.setTemperatureUnit("C"));
  });

  test("filters from the URL, clears no results, and preserves unrelated query parameters", async () => {
    showPage("/modules?tank=missing&other=keep");
    expect(screen.getByText("No modules match these filters.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() => expect(screen.getByText("24 modules")).toBeInTheDocument());
    expect(screen.getByTestId("location")).toHaveTextContent("other=keep");
    expect(screen.getByTestId("location")).not.toHaveTextContent("tank=missing");
  });

  test("keeps list state through keyboard modal entry, board inspection and dismissal", async () => {
    mocks.minerRoot = "/miners/ct1";
    showPage("/miners/ct1/modules?status=healthy&sort=name&direction=desc");
    const row = screen.getByText("T2-P2").closest("tr")!;
    fireEvent.keyDown(row, { key: "Enter" });
    const modal = await screen.findByRole("dialog", { name: "Modules" });
    expect(screen.getByTestId("location")).toHaveTextContent("/miners/ct1/modules/2/2?status=healthy");
    expect(within(modal).getByText("T2-P2")).toBeInTheDocument();
    expect(within(modal).getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tank 2");
    fireEvent.click(within(modal).getAllByRole("button", { name: "View" })[0]);
    const boardModal = await screen.findByRole("dialog", { name: "Hashboards" });
    expect(screen.getByTestId("location")).toHaveTextContent("/2/2/T2-M2-HB1?status=healthy");
    expect(within(boardModal).getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tank 2/Module 2");
    fireEvent.click(within(boardModal).getByRole("button", { name: "Board 1" }));
    fireEvent.click(await screen.findByRole("button", { name: "Board 2" }));
    expect(within(boardModal).getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tank 2/Module 2");
    expect(screen.getByTestId("location")).toHaveTextContent("/2/2/T2-M2-HB2?status=healthy&sort=name&direction=desc");
    fireEvent.click(within(boardModal).getByRole("button", { name: "Back to module" }));
    fireEvent.click(
      within(await screen.findByRole("dialog", { name: "Modules" })).getByRole("button", { name: "Done" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/miners/ct1/modules?status=healthy&sort=name&direction=desc",
    );
    expect(screen.getByText("T2-P2").closest("tr")).toBe(row);
    expect(row).toHaveFocus();
  });

  test("handles invalid deep links without opening the wrong module", () => {
    showPage("/modules/99/1");
    expect(screen.getByText(/Module not found/)).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Back to modules" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/modules");
  });

  test.each(["", "/miners/ct1"])("direct module and board links resolve location under %s", async (root) => {
    mocks.minerRoot = root;
    showPage(`${root}/modules/2/2/T2-M2-HB3`);
    const board = await screen.findByRole("dialog", { name: "Hashboards" });
    expect(within(board).getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tank 2/Module 2");
    fireEvent.click(within(board).getByRole("button", { name: "Back to module" }));
    const module = await screen.findByRole("dialog", { name: "Modules" });
    expect(within(module).getByRole("navigation", { name: "Breadcrumb" })).toHaveTextContent("Tank 2");
  });

  test("can return from an invalid board deep link to its module", async () => {
    showPage("/modules/1/1/missing?status=healthy");
    const modal = await screen.findByRole("dialog", { name: "Hashboards" });
    expect(within(modal).getByRole("status")).toHaveTextContent("Hashboard not found");
    expect(within(modal).queryByRole("navigation", { name: "Breadcrumb" })).not.toBeInTheDocument();
    fireEvent.click(within(modal).getByRole("button", { name: "Back to module" }));
    expect(await screen.findByRole("dialog", { name: "Modules" })).toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/modules/1/1?status=healthy");
  });

  test("shows stale data and separate empty, loading and failure states", () => {
    mocks.error = "failed";
    const view = showPage();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not refresh module readings");
    expect(screen.getByText("T1-P1")).toBeInTheDocument();
    view.unmount();
    useMinerStore.getState().resetDeviceData();
    showPage();
    expect(screen.getByRole("status")).toHaveTextContent("Loading modules");
  });
});
