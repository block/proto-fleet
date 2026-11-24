import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import Chip from "@/shared/components/Chip/Chip.tsx";

const chipText = "Alerts";

describe("Chip", () => {
  const onClick = vi.fn();

  test("calls onClick when the chip is clicked", () => {
    const { getByText } = render(<Chip onClick={onClick}>{chipText}</Chip>);
    const chipElement = getByText(chipText);
    fireEvent.click(chipElement);
    expect(onClick).toHaveBeenCalled();
  });
});
