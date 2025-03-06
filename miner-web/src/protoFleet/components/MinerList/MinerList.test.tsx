import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import MinerList from "./MinerList";
import { Miner } from "./types";

beforeAll(() => {
  vi.mock("recharts", () => ({
    ResponsiveContainer: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-responsive-container">{children}</div>
    ),
    LineChart: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-line-chart">{children}</div>
    ),
    ReferenceLine: () => <div data-testid="recharts-reference-line" />,
    Line: () => <div data-testid="recharts-line" />,
    XAxis: () => <div data-testid="recharts-xaxis" />,
    YAxis: () => <div data-testid="recharts-yaxis" />,
  }));
});

describe("MinerList", () => {
  const mockMiners: Miner[] = [
    {
      ip: "172.27.244.166",
      name: "C1-M01",
      macAddress: "0a:04:8a:54:fa:9f",
      hashrate: [
        { time: 1641024000000, hashrate: 189 },
        { time: 1641110400000, hashrate: 194 },
        { time: 1641196800000, hashrate: 190 },
        { time: 1641283200000, hashrate: 213.2 },
      ],
      efficiency: 15.5,
      powerUsage: 3.5,
      temperature: 65.5,
      status: {
        hashboard: "normal",
        asic: "normal",
        fans: "normal",
        cb: "normal",
        hashing: true,
        offline: false,
        asleep: false,
        broken: false,
      },
    },
    {
      ip: "172.27.244.166",
      name: "C1-M02",
      macAddress: "0b:04:8a:54:fa:9f",
      hashrate: [
        { time: 1641024000000, hashrate: 160 },
        { time: 1641110400000, hashrate: 163 },
        { time: 1641196800000, hashrate: 165 },
        { time: 1641283200000, hashrate: 150.8 },
      ],
      efficiency: 15.5,
      powerUsage: 3.5,
      temperature: 65.5,
      status: {
        hashboard: "warning",
        asic: "normal",
        fans: "normal",
        cb: "normal",
        hashing: true,
        offline: false,
        asleep: true,
        broken: false,
      },
    },
    {
      ip: "172.27.244.166",
      name: "C1-M03",
      macAddress: "0c:04:8a:54:fa:9f",
      hashrate: [
        { time: 1641024000000, hashrate: 184 },
        { time: 1641110400000, hashrate: 196 },
        { time: 1641196800000, hashrate: 194 },
        { time: 1641283200000, hashrate: 187 },
      ],
      efficiency: 15.5,
      powerUsage: 3.5,
      temperature: 65.5,
      status: {
        hashboard: "normal",
        asic: "normal",
        fans: "normal",
        cb: "normal",
        hashing: false,
        offline: false,
        asleep: false,
        broken: true,
      },
    },
    {
      ip: "172.27.244.166",
      name: "C1-M04",
      macAddress: "0e:04:8a:54:fa:9f",
      hashrate: [
        { time: 1641024000000, hashrate: 184 },
        { time: 1641110400000, hashrate: 196 },
        { time: 1641196800000, hashrate: 194 },
        { time: 1641283200000, hashrate: 152.3 },
      ],
      efficiency: 15.5,
      powerUsage: 3.5,
      temperature: 65.5,
      status: {
        hashboard: "normal",
        asic: "normal",
        fans: "normal",
        cb: "normal",
        hashing: true,
        offline: true,
        asleep: false,
        broken: false,
      },
    },
  ];

  it("renders rows correctly", () => {
    render(<MinerList title="Miners" miners={mockMiners} />);
    expect(screen.getAllByRole("row")).toHaveLength(mockMiners.length + 1);
  });

  it("selects all miners when clicking select all checkbox", () => {
    const { getByTestId } = render(
      <MinerList title="Miners" miners={mockMiners} />,
    );
    const selectAllCheckbox = getByTestId("miner-list-header").querySelector(
      "input[type='checkbox']",
    ) as HTMLInputElement;

    const selectMinerCheckboxes = getByTestId(
      "miner-list-body",
    ).querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // expect select all checkbox to be unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);

    // click individual miner checkboxes and make sure select all checkbox is unchecked and total checked is only 1
    fireEvent.click(selectMinerCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(1);

    // click select all checkboxes and make sure all checkboxes are checked
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(mockMiners.length);

    // click miner 1 (deselect) checkbox and make select all checkbox unchecked
    fireEvent.click(selectMinerCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(mockMiners.length - 1);

    // click select all twice to deselect all miners
    fireEvent.click(selectAllCheckbox);
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);
  });
});
