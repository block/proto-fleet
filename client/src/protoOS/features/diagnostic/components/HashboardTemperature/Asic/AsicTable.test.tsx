import { useState } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { AsicMetricProvider, type SelectedMetric } from "../AsicMetricContext";
import AsicTable from "./AsicTable";
import { CONTAINER_MODULE_LAYOUT, RIG_LAYOUT } from "@/protoOS/features/diagnostic/hashboardLayout/layouts";
import type { AsicData } from "@/protoOS/store";

const mocks = vi.hoisted(() => ({ rig: false }));
vi.mock("@/protoOS/features/diagnostic/hashboardLayout", () => ({
  useHashboardLayout: () => (mocks.rig ? RIG_LAYOUT : CONTAINER_MODULE_LAYOUT),
}));
vi.mock("@/shared/hooks/useCssVariable", () => ({
  default: (name: string) => (name.includes("success") ? "rgb(0 130 60)" : "rgb(0 100 200)"),
}));

const samples = (hashrate = 300): AsicData[] =>
  Array.from(
    { length: 312 },
    (_, index) =>
      ({
        id: `HB-1-asic-${index}`,
        hashboardSerial: "HB-1",
        index,
        row: Math.floor(index / 26),
        column: index % 26,
        temperature: { latest: { value: 70, units: "°C" } },
        hashrate: { latest: { value: index ? 100 : hashrate, units: "GH/s" } },
        frequency: { latest: { value: 500 + index, units: "MHz" } },
        voltage: { latest: { value: 0.8 + index / 10000, units: "V" } },
      }) as AsicData,
  );

function Harness({
  asics = samples(),
  metric = "temperature",
  serial = "HB-1",
}: {
  asics?: AsicData[];
  metric?: SelectedMetric;
  serial?: string;
}) {
  const [open, setOpen] = useState<string>();
  return (
    <AsicMetricProvider selectedMetric={metric}>
      <AsicTable
        asics={asics}
        hashboardSerialNumber={serial}
        pending={false}
        showPopover={open}
        setShowPopover={setOpen}
      />
    </AsicMetricProvider>
  );
}

beforeEach(() => {
  mocks.rig = false;
});

test("opens a live-only popover, moves between cells, updates readings and dismisses", async () => {
  const user = userEvent.setup();
  const { rerender } = render(<Harness />);
  const first = screen.getAllByRole("button")[0];
  await user.click(first);
  const popover = screen.getByTestId("asic-popover");
  expect(popover).toHaveTextContent("300.0 GH/s");
  expect(popover.querySelector(".recharts-wrapper")).toBeNull();
  expect(within(popover).queryByRole("progressbar")).not.toBeInTheDocument();
  expect(popover).not.toHaveTextContent("No chart data");
  rerender(<Harness asics={samples(320)} />);
  expect(screen.getByTestId("asic-popover")).toHaveTextContent("320.0 GH/s");
  await user.click(screen.getAllByRole("button")[1]);
  expect(screen.getAllByTestId("asic-popover")).toHaveLength(1);
  expect(screen.getByTestId("asic-popover")).toHaveTextContent("F2");
  await user.keyboard("{Escape}");
  expect(screen.queryByTestId("asic-popover")).not.toBeInTheDocument();
  first.focus();
  await user.keyboard("{Enter}");
  expect(screen.getByTestId("asic-popover")).toBeInTheDocument();
  await user.click(first);
  expect(screen.queryByTestId("asic-popover")).not.toBeInTheDocument();
  await user.click(first);
  fireEvent.mouseDown(document.body);
  expect(screen.queryByTestId("asic-popover")).not.toBeInTheDocument();
});

test("changes metric colors and the accessible reading", () => {
  const { rerender } = render(<Harness />);
  const first = screen.getAllByRole("button")[0];
  expect(within(first).getByText("F1")).toBeInTheDocument();
  expect(first).toHaveAccessibleName(/Temperature.*70/);
  const temperatureColor = first.style.backgroundColor;
  rerender(<Harness metric="hashrate" />);
  expect(first.style.backgroundColor).not.toBe(temperatureColor);
  expect(first).toHaveAccessibleName(/Hashrate.*300/);
  expect(screen.queryByTestId("asic-metric-legend")).not.toBeInTheDocument();
  for (const metric of ["frequency", "voltage"] as const) {
    rerender(<Harness metric={metric} />);
    expect(first.style.backgroundColor).not.toBe(temperatureColor);
    expect(screen.queryByTestId("asic-metric-legend")).not.toBeInTheDocument();
  }
});

test("shows zeros and missing readings, ignores history, and clears a selection on board changes", async () => {
  const asics = samples(0);
  asics[0].temperature = undefined;
  asics[0].hashrate = {
    latest: { value: 0, units: "GH/s" },
    timeSeries: { units: "GH/s", bucketSizeMs: 60_000, points: [{ timestampMs: 1_000, value: 999 }] },
  };
  const { rerender } = render(<Harness asics={asics} metric="hashrate" />);
  await userEvent.click(screen.getAllByRole("button")[0]);
  expect(screen.getByTestId("asic-popover")).toHaveTextContent("0.0 GH/s");
  expect(screen.getByTestId("asic-popover")).toHaveTextContent("—");
  rerender(<Harness serial="HB-2" asics={samples()} />);
  expect(screen.queryByTestId("asic-popover")).not.toBeInTheDocument();
  rerender(<Harness serial="HB-1" />);
  expect(screen.queryByTestId("asic-popover")).not.toBeInTheDocument();
});

test("retains printed readings for rigs", () => {
  mocks.rig = true;
  render(<Harness />);
  expect(within(screen.getAllByRole("button")[0]).getByText("70.0")).toBeInTheDocument();
});

test("keeps missing readings accessible without a legend", () => {
  const asics = samples().map((asic) => ({ ...asic, voltage: undefined }));
  render(<Harness asics={asics} metric="voltage" />);
  expect(screen.getAllByRole("button")[0]).toHaveAccessibleName("ASIC F1, Voltage —");
  expect(screen.queryByTestId("asic-metric-legend")).not.toBeInTheDocument();
});

test("recalculates hashrate colors from the current board's live readings", () => {
  const asics = samples(1000).map<AsicData>((asic, index) => ({
    ...asic,
    hashrate: { latest: { value: index === 0 ? 1000 : 800 + index, units: "GH/s" } },
  }));
  const { rerender } = render(<Harness asics={asics} metric="hashrate" />);
  const first = screen.getAllByRole("button")[0];
  const color = first.style.backgroundColor;
  const changed = asics.map<AsicData>((asic, index) => ({
    ...asic,
    hashrate: { latest: { value: index === 0 ? 1000 : 1100 + index, units: "GH/s" } },
  }));
  rerender(<Harness asics={changed} metric="hashrate" />);
  expect(first.style.backgroundColor).not.toBe(color);
  expect(first).toHaveAccessibleName(/Hashrate.*1,000/);
  rerender(<Harness asics={asics} metric="hashrate" serial="HB-2" />);
  expect(first.style.backgroundColor).toBe(color);
});

test.each([500, 1000, 1500])("uses the same relative hashrate ramp on CT1 and R2 at %s GH/s", (hashrate) => {
  const asics = samples(hashrate);
  const { rerender } = render(<Harness asics={asics} metric="hashrate" />);
  const ct1Color = screen.getAllByRole("button")[0].style.backgroundColor;
  mocks.rig = true;
  rerender(<Harness asics={asics} metric="hashrate" />);
  expect(screen.getAllByRole("button")[0].style.backgroundColor).toBe(ct1Color);
});
