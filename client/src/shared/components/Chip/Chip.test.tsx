import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import Chip from "@/shared/components/Chip/Chip.tsx";

const chipText = "Alerts";

describe("Chip", () => {
  const onClick = vi.fn();

  test("renders the chip with the correct text", () => {
    const { getByText } = render(<Chip>{chipText}</Chip>);
    const chipElement = getByText(chipText);
    expect(chipElement).toBeDefined();
  });

  test("calls onClick when the chip is clicked", () => {
    const { getByText } = render(<Chip onClick={onClick}>{chipText}</Chip>);
    const chipElement = getByText(chipText);
    fireEvent.click(chipElement);
    expect(onClick).toHaveBeenCalled();
  });
});
