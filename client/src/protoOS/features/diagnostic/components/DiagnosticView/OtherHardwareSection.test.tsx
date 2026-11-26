import { MemoryRouter } from "react-router-dom";
import { act, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import { OtherHardwareSection } from "./DiagnosticView";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: vi.fn() }));

const getMiningStatus = vi.fn(() => new Promise(() => {}));

const moduleReadings = (firstPower: number) =>
  toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [firstPower, 4300].map((inputPower, index) => ({
              id: index + 1,
              slots: [
                {
                  hashboard: {
                    serial: `board-${index + 1}`,
                    health: { timestamp: new Date().toISOString(), psuElectrical: { inputPower } },
                  },
                },
              ],
            })),
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );

beforeEach(() => {
  getMiningStatus.mockClear();
  vi.mocked(useMinerHosting).mockReturnValue({
    adapter: { getMiningStatus },
    isFleetHosted: true,
  } as unknown as ReturnType<typeof useMinerHosting>);
  useMinerStore.getState().resetDeviceData();
  const fixture = moduleReadings(0);
  useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry, 1, fixture.hardware);
});

test("container PSU cards expose status details without fetching mining status on mount", () => {
  useMinerStore.getState().hardware.setControlBoard({ serial: "container-controller", boardId: "1" });
  render(
    <MemoryRouter>
      <OtherHardwareSection />
    </MemoryRouter>,
  );

  expect(screen.getAllByTestId("card")).toHaveLength(2);
  expect(screen.queryByText(/Controller|CPU capacity|Latency/)).not.toBeInTheDocument();
  expect(getMiningStatus).not.toHaveBeenCalled();
  expect(screen.getAllByRole("button", { name: "More info" })).toHaveLength(2);
});

test("keeps module PSUs distinct when they share slot 1 and updates their own readings", () => {
  const errors = vi.spyOn(console, "error").mockImplementation(() => {});
  try {
    render(
      <MemoryRouter>
        <OtherHardwareSection />
      </MemoryRouter>,
    );
    const [first, second] = screen.getAllByTestId("card");
    expect(within(first).getByText("0.0 kW")).toBeInTheDocument();
    expect(within(second).getByText("4.3 kW")).toBeInTheDocument();

    act(() => useMinerStore.getState().telemetry.mergeTelemetry(moduleReadings(1000).telemetry, 2));
    expect(within(first).getByText("1.0 kW")).toBeInTheDocument();
    expect(within(second).getByText("4.3 kW")).toBeInTheDocument();
    expect(errors).not.toHaveBeenCalled();
  } finally {
    errors.mockRestore();
  }
});
