import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import { computeNestedPosition } from "./useFilterDropdownPosition";

const rect = (overrides: Partial<DOMRect>): DOMRect => {
  const base = { x: 0, y: 0, width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0 };
  const merged = { ...base, ...overrides };
  return { ...merged, toJSON: () => merged } as DOMRect;
};

const buildCategories = (overrides: Partial<Record<string, FilterCategory>> = {}): FilterCategory[] => [
  {
    key: "status",
    label: "Status",
    options: [
      { id: "hashing", label: "Hashing" },
      { id: "offline", label: "Offline" },
    ],
    selectedValues: [],
    ...overrides.status,
  },
  {
    key: "firmware",
    label: "Firmware",
    options: [
      { id: "v3.5.1", label: "v3.5.1" },
      { id: "v3.5.2", label: "v3.5.2" },
    ],
    selectedValues: [],
    ...overrides.firmware,
  },
  {
    key: "zone",
    label: "Zones",
    options: [],
    selectedValues: [],
    ...overrides.zone,
  },
];

describe("NestedDropdownFilter", () => {
  it("renders the trigger and reveals categories on click", () => {
    const onChange = vi.fn();
    const onClearAll = vi.fn();

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={buildCategories()}
        onChange={onChange}
        onClearAll={onClearAll}
      />,
    );

    expect(screen.getByTestId("nested-dropdown-filter")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    expect(screen.getByTestId("nested-dropdown-filter-popover")).toBeInTheDocument();
    expect(screen.getByTestId("nested-dropdown-filter-row-status")).toBeInTheDocument();
    expect(screen.getByTestId("nested-dropdown-filter-row-firmware")).toBeInTheDocument();
    expect(screen.getByTestId("nested-dropdown-filter-row-zone")).toBeInTheDocument();
  });

  it("uses the provided label on the trigger button", () => {
    render(
      <NestedDropdownFilter
        label="More filters"
        categories={buildCategories()}
        onChange={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    expect(screen.getByText("More filters")).toBeInTheDocument();
  });

  it("renders a per-category count badge on each row that has selections", () => {
    render(
      <NestedDropdownFilter
        label="Filters"
        categories={buildCategories({
          status: {
            key: "status",
            label: "Status",
            options: [
              { id: "hashing", label: "Hashing" },
              { id: "offline", label: "Offline" },
            ],
            selectedValues: ["hashing", "offline"],
          },
          firmware: {
            key: "firmware",
            label: "Firmware",
            options: [{ id: "v3.5.1", label: "v3.5.1" }],
            selectedValues: ["v3.5.1"],
          },
        })}
        onChange={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    const statusRow = screen.getByTestId("nested-dropdown-filter-row-status");
    expect(statusRow).toHaveTextContent("2");
    const firmwareRow = screen.getByTestId("nested-dropdown-filter-row-firmware");
    expect(firmwareRow).toHaveTextContent("1");
  });

  it("disables the row and shows '(no values)' when a category has no options", () => {
    render(
      <NestedDropdownFilter label="Filters" categories={buildCategories()} onChange={vi.fn()} onClearAll={vi.fn()} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    const zoneRow = screen.getByTestId("nested-dropdown-filter-row-zone");
    expect(zoneRow).toBeDisabled();
    expect(screen.getByText("(no values)")).toBeInTheDocument();
  });

  it("opens a nested submenu and propagates selection via onChange", async () => {
    const onChange = vi.fn();

    render(
      <NestedDropdownFilter label="Filters" categories={buildCategories()} onChange={onChange} onClearAll={vi.fn()} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-firmware"));

    await waitFor(() => {
      expect(screen.getByText("v3.5.1")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("filter-option-v3.5.1"));

    expect(onChange).toHaveBeenCalledWith("firmware", ["v3.5.1"]);
  });

  it("calls onClearAll only when the footer button fires", () => {
    const onClearAll = vi.fn();

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={buildCategories({
          status: {
            key: "status",
            label: "Status",
            options: [{ id: "hashing", label: "Hashing" }],
            selectedValues: ["hashing"],
          },
        })}
        onChange={vi.fn()}
        onClearAll={onClearAll}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    fireEvent.click(screen.getByText("Clear all"));

    expect(onClearAll).toHaveBeenCalledTimes(1);
  });

  it("hides Clear all when no filters are active", () => {
    render(
      <NestedDropdownFilter label="Filters" categories={buildCategories()} onChange={vi.fn()} onClearAll={vi.fn()} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    expect(screen.queryByText("Clear all")).not.toBeInTheDocument();
  });
});

describe("computeNestedPosition", () => {
  // Outer popover sits in the upper-left area of a roomy viewport so the row
  // sits near the bottom of the parent surface.
  const parent = rect({ left: 16, top: 100, right: 336, bottom: 580, width: 320, height: 480 });

  it("anchors to the parent's right edge with a 2px gap when there is room", () => {
    const row = rect({ left: 36, top: 200, right: 316, bottom: 240, width: 280, height: 40 });
    const pos = computeNestedPosition(parent, row, 240, 1280, 800);

    expect(pos.left).toBe(parent.right + 2);
    expect(pos.top).toBe(row.top);
    expect(pos.maxHeight).toBeUndefined();
  });

  it("flips to the left side when the right side would overflow the viewport", () => {
    const row = rect({ left: 36, top: 200, right: 316, bottom: 240, width: 280, height: 40 });
    // viewport too narrow for parent.right + gap + 320
    const pos = computeNestedPosition(parent, row, 240, 600, 800);

    // 320 panel should land to the left of the parent (parent.left - gap - 320)
    expect(pos.left).toBe(Math.max(16, parent.left - 2 - 320));
  });

  it("shifts top upward so a short panel fits without overflowing the viewport bottom", () => {
    // Row is near the bottom of the parent popover on a 600px viewport.
    // Content is 240px tall — fits in the viewport but row.top would push the bottom off-screen.
    const row = rect({ left: 36, top: 450, right: 316, bottom: 490, width: 280, height: 40 });
    const viewportHeight = 600;
    const contentHeight = 240;
    const pos = computeNestedPosition(parent, row, contentHeight, 1280, viewportHeight);

    // Top is shifted up so the bottom edge stays inside the viewport.
    expect(pos.top).toBeLessThan(row.top);
    expect(pos.top + contentHeight).toBeLessThanOrEqual(viewportHeight - 16);
    // Content fits naturally — no maxHeight clamp.
    expect(pos.maxHeight).toBeUndefined();
  });

  it("clips with maxHeight only when natural content exceeds the viewport", () => {
    const row = rect({ left: 36, top: 100, right: 316, bottom: 140, width: 280, height: 40 });
    const viewportHeight = 400;
    // Way more content than viewport can hold.
    const contentHeight = 800;
    const pos = computeNestedPosition(parent, row, contentHeight, 1280, viewportHeight);

    // maxHeight is set to the available viewport space.
    expect(pos.maxHeight).toBe(viewportHeight - 32);
    // Pinned at the safe top edge.
    expect(pos.top).toBe(16);
  });

  it("uses the soft minimum on the first pass before the panel is measured", () => {
    const row = rect({ left: 36, top: 460, right: 316, bottom: 500, width: 280, height: 40 });
    const viewportHeight = 500;
    const pos = computeNestedPosition(parent, row, null, 1280, viewportHeight);

    // First-pass top assumes a 120px panel and shifts up if needed.
    expect(pos.top).toBeLessThan(row.top);
    // No clamp applied yet — measurement hasn't run.
    expect(pos.maxHeight).toBeUndefined();
  });
});
