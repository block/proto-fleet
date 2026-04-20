import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Grip from "./Grip";

describe("Grip", () => {
  it("uses the Figma asset geometry with a square-frame footprint that matches the UI", () => {
    const { container } = render(<Grip width="w-3" className="h-3 shrink-0" />);

    expect(container.firstChild).toHaveClass("w-3", "h-3", "shrink-0");
    expect(container.firstChild).toHaveClass("relative");
    expect(container.querySelector("svg")).toHaveAttribute("viewBox", "0 0 16.666 10");
    expect(container.querySelector("svg")).toHaveAttribute("preserveAspectRatio", "none");
    expect(container.querySelector("svg")).toHaveClass(
      "absolute",
      "top-[16.666667%]",
      "left-0",
      "h-[66.666667%]",
      "w-full",
      "rotate-90",
    );
  });
});
