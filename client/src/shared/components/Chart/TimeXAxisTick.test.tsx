import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import TimeXAxisTick from "./TimeXAxisTick";

describe("TimeXAxisTick", () => {
  it("renders the hover timestamp when tooltip value is mapped to a synthetic tick", () => {
    const hoverDatetime = Date.UTC(2026, 0, 1, 12, 30, 0);
    const syntheticTick = Date.UTC(2026, 0, 1, 12, 0, 0);

    const { container } = render(
      <svg>
        <TimeXAxisTick
          dataPointCount={6}
          maxTicksToShow={6}
          visibleTicksCount={6}
          tooltipDatetime={hoverDatetime}
          tooltipTickValue={syntheticTick}
          hideNonTooltipTicks
          payload={{ value: syntheticTick, index: 2, offset: 0 }}
          x={120}
          y={20}
        />
      </svg>,
    );

    expect(container.textContent).toContain("•");
  });

  it("hides non-hover ticks when tooltip mode is active", () => {
    const hoverDatetime = Date.UTC(2026, 0, 1, 12, 30, 0);
    const mappedTick = Date.UTC(2026, 0, 1, 12, 0, 0);
    const nonMappedTick = Date.UTC(2026, 0, 1, 11, 0, 0);

    const { container } = render(
      <svg>
        <TimeXAxisTick
          dataPointCount={6}
          maxTicksToShow={6}
          visibleTicksCount={6}
          tooltipDatetime={hoverDatetime}
          tooltipTickValue={mappedTick}
          hideNonTooltipTicks
          payload={{ value: nonMappedTick, index: 1, offset: 0 }}
          x={80}
          y={20}
        />
      </svg>,
    );

    expect(container.textContent).toBe("");
  });
});
