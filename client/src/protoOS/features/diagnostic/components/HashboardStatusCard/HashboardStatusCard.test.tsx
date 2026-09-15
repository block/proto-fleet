import { MemoryRouter } from "react-router-dom";
import { act, render, screen } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import HashboardStatusCard from "./HashboardStatusCard";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";

vi.mock("@/protoOS/components/StatusModal", () => ({ ProtoOSStatusModal: () => null }));

const boardId = getHashboardId("HB-1");

beforeEach(() => {
  const store = useMinerStore.getState();
  store.resetDeviceData();
  store.systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1", product: "CT1" } }));
  store.hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
});

const renderCard = () =>
  render(
    <MemoryRouter>
      <HashboardStatusCard identifier="HB-1" />
    </MemoryRouter>,
  );

const setReadings = (power?: number, hashrate?: number, efficiency?: number | null) =>
  useMinerStore.setState((state) => {
    state.telemetry.hashboards.set(boardId, {
      hashboardId: boardId,
      power: power === undefined ? undefined : { latest: { value: power, units: "W" } },
      hashrate: hashrate === undefined ? undefined : { latest: { value: hashrate, units: "TH/s" } },
      efficiency: efficiency === undefined ? undefined : { latest: { value: efficiency, units: "J/TH" } },
    });
  });

const efficiencyText = () => screen.getByText("Efficiency").parentElement;

test.each([undefined, null])("calculates card efficiency when the stored value is %s", (efficiency) => {
  setReadings(1500, 100, efficiency);
  renderCard();
  expect(efficiencyText()).toHaveTextContent("15.0 J/TH");
  act(() => setReadings(1600, 100, efficiency));
  expect(efficiencyText()).toHaveTextContent("16.0 J/TH");
  act(() => setReadings(1600, 0, efficiency));
  expect(efficiencyText()).toHaveTextContent("—");
});

test("preserves reported efficiency and real zero power", () => {
  setReadings(1500, 100, 16);
  renderCard();
  expect(efficiencyText()).toHaveTextContent("16.0 J/TH");
  act(() => setReadings(0, 100));
  expect(efficiencyText()).toHaveTextContent("0.0 J/TH");
});

test.each([
  [undefined, 100],
  [1500, undefined],
  [1500, 0],
  [1500, -1],
  [-1, 100],
  [Infinity, 100],
  [1500, NaN],
])("keeps a dash when efficiency cannot be calculated from %s W and %s TH/s", (power, hashrate) => {
  setReadings(power, hashrate);
  renderCard();
  expect(efficiencyText()).toHaveTextContent("—");
});
