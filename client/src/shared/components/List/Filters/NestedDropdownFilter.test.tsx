import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import NestedDropdownFilter, { type FilterCategory } from "./NestedDropdownFilter";
import { computeNestedPosition } from "./useFilterDropdownPosition";

const mockedDimensions = {
  width: 1280,
  height: 800,
  isPhone: false,
  isTablet: false,
  isLaptop: false,
  isDesktop: true,
};

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockedDimensions,
}));

const setViewport = (overrides: Partial<typeof mockedDimensions>) => {
  Object.assign(mockedDimensions, overrides);
};

const resetViewport = () => {
  Object.assign(mockedDimensions, {
    width: 1280,
    height: 800,
    isPhone: false,
    isTablet: false,
    isLaptop: false,
    isDesktop: true,
  });
};

const rect = (overrides: Partial<DOMRect>): DOMRect => {
  const base = { x: 0, y: 0, width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0 };
  const merged = { ...base, ...overrides };
  return { ...merged, toJSON: () => merged } as DOMRect;
};

const checkbox = (
  key: string,
  label: string,
  optionList: { id: string; label: string }[],
  selectedValues: string[] = [],
): FilterCategory => ({
  kind: "checkbox",
  key,
  label,
  options: optionList,
  selectedValues,
});

const defaultCategories: FilterCategory[] = [
  checkbox("status", "Status", [
    { id: "hashing", label: "Hashing" },
    { id: "offline", label: "Offline" },
  ]),
  checkbox("firmware", "Firmware", [
    { id: "v3.5.1", label: "v3.5.1" },
    { id: "v3.5.2", label: "v3.5.2" },
  ]),
  checkbox("zone", "Zones", []),
];

const noopCallbacks = () => ({
  onCheckboxChange: vi.fn(),
  onRequestEdit: vi.fn(),
});

describe("NestedDropdownFilter", () => {
  it("renders the trigger and reveals categories on click", () => {
    render(
      <NestedDropdownFilter label="Filters" categories={defaultCategories} {...noopCallbacks()} onClearAll={vi.fn()} />,
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
        categories={defaultCategories}
        {...noopCallbacks()}
        onClearAll={vi.fn()}
      />,
    );

    expect(screen.getByText("More filters")).toBeInTheDocument();
  });

  it("renders a per-category count badge on each row that has selections", () => {
    const categories: FilterCategory[] = [
      checkbox(
        "status",
        "Status",
        [
          { id: "hashing", label: "Hashing" },
          { id: "offline", label: "Offline" },
        ],
        ["hashing", "offline"],
      ),
      checkbox("firmware", "Firmware", [{ id: "v3.5.1", label: "v3.5.1" }], ["v3.5.1"]),
      checkbox("zone", "Zones", []),
    ];

    render(<NestedDropdownFilter label="Filters" categories={categories} {...noopCallbacks()} onClearAll={vi.fn()} />);

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    const statusRow = screen.getByTestId("nested-dropdown-filter-row-status");
    expect(statusRow).toHaveTextContent("2");
    const firmwareRow = screen.getByTestId("nested-dropdown-filter-row-firmware");
    expect(firmwareRow).toHaveTextContent("1");
  });

  it("disables the row and shows '(no values)' when a category has no options", () => {
    render(
      <NestedDropdownFilter label="Filters" categories={defaultCategories} {...noopCallbacks()} onClearAll={vi.fn()} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    const zoneRow = screen.getByTestId("nested-dropdown-filter-row-zone");
    expect(zoneRow).toBeDisabled();
    expect(screen.getByText("(no values)")).toBeInTheDocument();
  });

  it("opens a nested submenu and propagates selection via onCheckboxChange", async () => {
    const onCheckboxChange = vi.fn();

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={defaultCategories}
        onCheckboxChange={onCheckboxChange}
        onRequestEdit={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-firmware"));

    await waitFor(() => {
      expect(screen.getByText("v3.5.1")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("filter-option-v3.5.1"));

    expect(onCheckboxChange).toHaveBeenCalledWith("firmware", ["v3.5.1"]);
  });

  it("calls onClearAll only when the footer button fires", () => {
    const onClearAll = vi.fn();
    const categories: FilterCategory[] = [
      checkbox("status", "Status", [{ id: "hashing", label: "Hashing" }], ["hashing"]),
    ];

    render(
      <NestedDropdownFilter label="Filters" categories={categories} {...noopCallbacks()} onClearAll={onClearAll} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByText("Clear all"));

    expect(onClearAll).toHaveBeenCalledTimes(1);
  });

  it("hides Clear all when no filters are active", () => {
    render(
      <NestedDropdownFilter label="Filters" categories={defaultCategories} {...noopCallbacks()} onClearAll={vi.fn()} />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));

    expect(screen.queryByText("Clear all")).not.toBeInTheDocument();
  });
});

describe("NestedDropdownFilter on small viewports", () => {
  afterEach(() => {
    resetViewport();
  });

  it("replaces the category list with the selected category's options on click in mobile mode", async () => {
    setViewport({ isPhone: true, isDesktop: false, width: 375 });
    const onChange = vi.fn();

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={defaultCategories}
        onCheckboxChange={onChange}
        onRequestEdit={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-firmware"));

    await waitFor(() => {
      expect(screen.getByTestId("filter-option-v3.5.1")).toBeInTheDocument();
    });

    // Sibling category rows are no longer rendered — the options replaced them.
    expect(screen.queryByTestId("nested-dropdown-filter-row-status")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("filter-option-v3.5.1"));
    expect(onChange).toHaveBeenCalledWith("firmware", ["v3.5.1"]);
  });

  it("returns to the category list when the back affordance is clicked", async () => {
    setViewport({ isPhone: true, isDesktop: false, width: 375 });

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={defaultCategories}
        onCheckboxChange={vi.fn()}
        onRequestEdit={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-firmware"));

    await waitFor(() => {
      expect(screen.getByTestId("filter-option-v3.5.1")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("nested-dropdown-filter-back"));

    expect(screen.getByTestId("nested-dropdown-filter-row-status")).toBeInTheDocument();
    expect(screen.getByTestId("nested-dropdown-filter-row-firmware")).toBeInTheDocument();
    expect(screen.queryByTestId("filter-option-v3.5.1")).not.toBeInTheDocument();
  });

  it("does not portal a side-by-side submenu when in mobile mode", () => {
    setViewport({ isTablet: true, isDesktop: false, width: 800 });

    render(
      <NestedDropdownFilter
        label="Filters"
        categories={defaultCategories}
        onCheckboxChange={vi.fn()}
        onRequestEdit={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByTestId("nested-dropdown-filter"));
    fireEvent.click(screen.getByTestId("nested-dropdown-filter-row-firmware"));

    // The portaled side panel testId is reserved for the desktop hover layout.
    expect(screen.queryByTestId("nested-dropdown-filter-submenu-firmware")).not.toBeInTheDocument();
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
    // Viewport just narrow enough that parent.right + gap + NESTED_POPOVER_WIDTH (240)
    // doesn't fit on the right; the panel should flip to the left of the parent.
    const pos = computeNestedPosition(parent, row, 240, 500, 800);
    expect(pos.left).toBe(Math.max(16, parent.left - 2 - 240));
  });

  it("shifts top upward so a short panel fits without overflowing the viewport bottom", () => {
    const row = rect({ left: 36, top: 450, right: 316, bottom: 490, width: 280, height: 40 });
    const viewportHeight = 600;
    const contentHeight = 240;
    const pos = computeNestedPosition(parent, row, contentHeight, 1280, viewportHeight);
    expect(pos.top).toBeLessThan(row.top);
    expect(pos.top + contentHeight).toBeLessThanOrEqual(viewportHeight - 16);
    expect(pos.maxHeight).toBeUndefined();
  });

  it("clips with maxHeight only when natural content exceeds the viewport", () => {
    const row = rect({ left: 36, top: 100, right: 316, bottom: 140, width: 280, height: 40 });
    const viewportHeight = 400;
    const contentHeight = 800;
    const pos = computeNestedPosition(parent, row, contentHeight, 1280, viewportHeight);
    expect(pos.maxHeight).toBe(viewportHeight - 32);
    expect(pos.top).toBe(16);
  });

  it("uses the soft minimum on the first pass before the panel is measured", () => {
    const row = rect({ left: 36, top: 460, right: 316, bottom: 500, width: 280, height: 40 });
    const viewportHeight = 500;
    const pos = computeNestedPosition(parent, row, null, 1280, viewportHeight);
    expect(pos.top).toBeLessThan(row.top);
    expect(pos.maxHeight).toBeUndefined();
  });
});
