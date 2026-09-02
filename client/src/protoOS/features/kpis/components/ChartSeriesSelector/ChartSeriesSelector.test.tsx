import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ChartSeriesSelector from "./ChartSeriesSelector";

vi.mock("@/shared/hooks/useCssVariable", () => ({ default: (value: string) => value }));

const tanks = Array.from({ length: 6 }, (_, index) => ({
  key: `tank:${index + 1}`,
  label: String(index + 1),
  color: `--color-hashboard-${index + 1}`,
}));

describe("ChartSeriesSelector", () => {
  test("renders and selects all six tank series", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(
      <ChartSeriesSelector
        activeKeys={[]}
        aggregateKey="miner"
        onChange={onChange}
        segmentsLabel="tanks"
        series={tanks}
      />,
    );

    expect(screen.getByRole("button", { name: "Summary" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "All tanks" })).toBeInTheDocument();
    tanks.forEach(({ label }) => expect(screen.getByRole("button", { name: label })).toBeInTheDocument());

    await user.click(screen.getByTestId("chart-filter-all-tanks"));

    expect(onChange).toHaveBeenCalledWith(tanks.map(({ key }) => key));
  });

  test("selects one tank from the default state", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(
      <ChartSeriesSelector
        activeKeys={[]}
        aggregateKey="miner"
        onChange={onChange}
        segmentsLabel="tanks"
        series={tanks}
      />,
    );

    await user.click(screen.getByTestId("chart-filter-tank-3"));

    expect(onChange).toHaveBeenCalledWith(["tank:3"]);
  });
});
