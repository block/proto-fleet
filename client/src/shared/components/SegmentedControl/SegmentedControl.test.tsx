import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import SegmentedControl from "./SegmentedControl";
import type { Segment } from "@/shared/components/SegmentedControl/types";

describe("Segmented control", () => {
  const segments = [
    {
      key: "active",
      title: "Active",
    },
    {
      key: "archive",
      title: "Archive",
    },
  ] as Segment[];

  const onSelect = vi.fn();

  test("renders with provided segments", () => {
    const { getByText } = render(<SegmentedControl segments={segments} onSelect={onSelect} />);
    segments.forEach((segment) => {
      expect(getByText(segment.title)).toBeInTheDocument();
    });
  });

  test("calls onSelect callback when a segment is clicked", () => {
    const { getByText } = render(<SegmentedControl segments={segments} onSelect={onSelect} />);
    fireEvent.mouseDown(getByText(segments[1].title));
    expect(onSelect).toHaveBeenCalledWith(segments[1].key);
  });

  test("applies active class to the selected segment", () => {
    const { getByText } = render(<SegmentedControl segments={segments} onSelect={onSelect} />);
    const selectedSegment = getByText(segments[0].title);
    const selectedSegmentButton = selectedSegment.closest("button");
    expect(selectedSegmentButton).not.toBeNull();
    expect(selectedSegmentButton).toHaveClass("text-emphasis-200");

    const secondSegment = getByText(segments[1].title);
    const secondSegmentButton = secondSegment.closest("button");
    expect(secondSegmentButton).not.toBeNull();
    expect(secondSegmentButton).toHaveClass("text-200 text-text-primary-30");
  });

  test("does not break when no segments are provided", () => {
    const { container } = render(<SegmentedControl segments={[]} onSelect={onSelect} />);
    expect(container).toBeEmptyDOMElement();
  });
});
