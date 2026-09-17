import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import HashboardTemperatureWrapper from "./HashboardTemperatureWrapper";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ adapterId: "mdk-v2", hashboardStatus: vi.fn(() => ({ error: undefined })) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ resolution: { adapterId: mocks.adapterId } }),
}));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: mocks.hashboardStatus }));
vi.mock("./HashboardTemperature", () => ({ default: ({ serial }: { serial: string }) => <p>{serial}</p> }));

const NextHashboard = () => {
  const navigate = useNavigate();
  return <button onClick={() => navigate("../HB-2", { relative: "path" })}>Next hashboard</button>;
};

const showPage = (root: string) =>
  render(
    <MemoryRouter initialEntries={[`${root}/HB-1`]}>
      <Routes>
        <Route
          path={`${root}/:serial`}
          element={
            <>
              <HashboardTemperatureWrapper />
              <NextHashboard />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );

beforeEach(() => {
  vi.clearAllMocks();
  useMinerStore.getState().resetDeviceData();
  mocks.adapterId = "mdk-v2";
});

test.each(["T1-M1-HB1", "T1-M2-HB1"])("validates and polls a nested module board: %s", (serial) => {
  useMinerStore.getState().hardware.replaceHardware(moduleFixture().hardware);
  render(
    <MemoryRouter initialEntries={[`/tanks/1/modules/1/${serial}`]}>
      <Routes>
        <Route path="/tanks/:tankId/modules/:moduleId/:serial" element={<HashboardTemperatureWrapper />} />
      </Routes>
    </MemoryRouter>,
  );
  const valid = serial === "T1-M1-HB1";
  expect(mocks.hashboardStatus).toHaveBeenLastCalledWith({ hashboardSerialNumbers: valid ? [serial] : [], poll: true });
  if (!valid) expect(screen.getByRole("status")).toHaveTextContent("Hashboard not found in this module");
});

test.each(["/systems", "/temperature", "/miners/ct1/systems"])(
  "loads only the selected CT1 hashboard at %s",
  (root) => {
    showPage(root);
    expect(mocks.hashboardStatus).toHaveBeenLastCalledWith({ hashboardSerialNumbers: ["HB-1"], poll: false });
    fireEvent.click(screen.getByRole("button", { name: "Next hashboard" }));
    expect(screen.getByText("HB-2")).toBeInTheDocument();
    expect(mocks.hashboardStatus).toHaveBeenLastCalledWith({ hashboardSerialNumbers: ["HB-2"], poll: false });
  },
);

test("loads rig ASICs lazily for the selected board", () => {
  mocks.adapterId = "mdk-v1";
  showPage("/systems");
  expect(mocks.hashboardStatus).toHaveBeenCalledWith({ hashboardSerialNumbers: ["HB-1"], poll: false });
});
