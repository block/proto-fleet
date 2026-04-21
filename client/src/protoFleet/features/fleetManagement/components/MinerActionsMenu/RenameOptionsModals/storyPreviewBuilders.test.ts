import { describe, expect, it } from "vitest";

import { buildFixedPreview, buildQualifierPreview } from "./storyPreviewBuilders";
import { fixedStringSections } from "./types";

describe("storyPreviewBuilders", () => {
  it("keeps the original character order when selecting last characters", () => {
    const preview = buildFixedPreview({
      characterCount: 6,
      stringSection: fixedStringSections.last,
    });

    expect(preview.previewName).toBe("EXAUST-BA-R01-001-4D5E");
    expect(preview.highlightedText).toBe("EXAUST");
    expect(preview.highlightStartIndex).toBe(0);
  });

  it("uses the beginning of the section when selecting first characters", () => {
    const preview = buildFixedPreview({
      characterCount: 4,
      stringSection: fixedStringSections.first,
    });

    expect(preview.previewName).toBe("TEXA-BA-R01-001-4D5E");
    expect(preview.highlightedText).toBe("TEXA");
    expect(preview.highlightStartIndex).toBe(0);
  });

  it("builds qualifier preview with BA as the editable building section", () => {
    const preview = buildQualifierPreview({
      prefix: "",
      suffix: "",
    });

    expect(preview.previewName).toBe("TEXAUST-BA-R01-001-4D5E");
    expect(preview.highlightedText).toBe("BA");
    expect(preview.highlightStartIndex).toBe(8);
  });

  it("includes prefix and suffix in the highlighted text", () => {
    const preview = buildQualifierPreview({
      prefix: "as-",
      suffix: "-da",
    });

    expect(preview.previewName).toBe("TEXAUST-as-BA-da-R01-001-4D5E");
    expect(preview.highlightedText).toBe("as-BA-da");
    expect(preview.highlightStartIndex).toBe(8);
  });
});
