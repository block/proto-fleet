import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import HighlightedNamePreview from "./HighlightedNamePreview";

describe("HighlightedNamePreview", () => {
  it("highlights the matching substring when no explicit start index is provided", () => {
    render(<HighlightedNamePreview previewName="TEXA-BA-R01-001-4D5E" highlightedText="BA" />);

    expect(screen.getByTestId("highlighted-name-preview-leading")).toHaveTextContent("TEXA-");
    expect(screen.getByTestId("highlighted-name-preview-highlighted")).toHaveTextContent("BA");
    expect(screen.getByTestId("highlighted-name-preview-trailing")).toHaveTextContent("-R01-001-4D5E");
  });

  it("uses the explicit highlight start index when the same text appears earlier in the preview", () => {
    render(<HighlightedNamePreview previewName="AB-AB-R01" highlightedText="AB" highlightStartIndex={3} />);

    expect(screen.getByTestId("highlighted-name-preview-leading")).toHaveTextContent("AB-");
    expect(screen.getByTestId("highlighted-name-preview-highlighted")).toHaveTextContent("AB");
    expect(screen.getByTestId("highlighted-name-preview-trailing")).toHaveTextContent("-R01");
  });
});
