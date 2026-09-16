import { MemoryRouter, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { act, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import HashboardTemperature from "./HashboardTemperature";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";

vi.mock("@/protoOS/api", () => ({ useTelemetry: vi.fn() }));
vi.mock("./Asic/AsicTableWrapper", () => ({ default: () => <div>ASIC grid</div> }));
const id = getHashboardId("T1-M1-HB1");
const setReadings = (power: number, hashrate: number, efficiency?: number) =>
  useMinerStore.setState((state) => {
    state.telemetry.hashboards.set(id, {
      hashboardId: id,
      power: { latest: { value: power, units: "W" } },
      hashrate: { latest: { value: hashrate, units: "TH/s" } },
      efficiency: efficiency === undefined ? undefined : { latest: { value: efficiency, units: "J/TH" } },
    });
  });
function BoardPage() {
  const { serial } = useParams();
  return <HashboardTemperature serial={serial!} />;
}
function Location() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <p data-testid="location">{location.pathname}</p>
      <button onClick={() => navigate(-1)}>History back</button>
    </>
  );
}
function showPage() {
  return render(
    <MemoryRouter initialEntries={["/tanks/1/modules/1", "/tanks/1/modules/1/T1-M1-HB1"]}>
      <Location />
      <Routes>
        <Route path="/tanks/:tankId/modules/:moduleId/:serial" element={<BoardPage />} />
        <Route path="/tanks/:tankId/modules/:moduleId" element={<p>Module modal</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  const store = useMinerStore.getState();
  store.resetDeviceData();
  store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1", product: "CT1" } }));
  store.hardware.replaceHardware(moduleFixture().hardware);
  setReadings(1500, 100);
});

test("CT1 shows five ordered stats with live calculated or reported efficiency", () => {
  showPage();
  const stats = screen.getAllByTestId("stats-item");
  expect(stats).toHaveLength(5);
  expect(stats.map((item) => item.textContent)).toEqual([
    expect.stringContaining("High ASIC temp"),
    expect.stringContaining("Avg ASIC temp"),
    expect.stringContaining("Power usage"),
    expect.stringContaining("Efficiency15.0"),
    expect.stringContaining("Total board hashrate"),
  ]);
  act(() => setReadings(2000, 100, 17));
  expect(stats[3]).toHaveTextContent("17.0");
  act(() => setReadings(2000, 0));
  expect(stats[3]).toHaveTextContent("—");
});

test("switches boards within the module route using replace, and returns to the module", async () => {
  const user = userEvent.setup();
  showPage();
  await user.click(screen.getByRole("button", { name: "Board 1" }));
  await user.click(screen.getByText("Board 2"));
  expect(screen.getByTestId("location")).toHaveTextContent("/tanks/1/modules/1/T1-M1-HB2");
  expect(screen.getByText("ASIC grid")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History back" }));
  expect(screen.getByText("Module modal")).toBeInTheDocument();
});

test.each(["Back to module", "Done"])("%s returns to the module", async (name) => {
  showPage();
  await userEvent.click(screen.getByRole("button", { name }));
  expect(screen.getByText("Module modal")).toBeInTheDocument();
});

test("rig keeps its four stats and dismissal icon", () => {
  useMinerStore
    .getState()
    .systemInfo.setSystemInfo({ ...toCanonicalSystemInfo({ info: { id: "r2", product: "R2" } }), moduleModel: "r2" });
  showPage();
  expect(within(screen.getByTestId("stats-container")).getAllByTestId("stats-item")).toHaveLength(4);
  expect(screen.getByRole("button", { name: "Close hashboards" })).toBeInTheDocument();
});
