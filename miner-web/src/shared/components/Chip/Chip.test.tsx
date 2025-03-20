import { render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import Chip from "@/shared/components/Chip/Chip.tsx";

const chipText = "Alerts";

describe("Chip", () => {
  test("renders the chip with the correct text", () => {
    const { getByText } = render(<Chip>{chipText}</Chip>);
    const chipElement = getByText(chipText);
    expect(chipElement).toBeDefined();
  });
});
