import { describe, expect, it } from "vitest";

import { getObjectCoverSourceCrop, getObjectCoverSourceCropForRegion } from "./objectCoverSourceCrop";

describe("getObjectCoverSourceCrop", () => {
  it("crops a wide camera frame to the visible square preview", () => {
    expect(
      getObjectCoverSourceCrop({
        sourceWidth: 1920,
        sourceHeight: 1080,
        renderedWidth: 600,
        renderedHeight: 600,
      }),
    ).toEqual({
      sx: 420,
      sy: 0,
      sw: 1080,
      sh: 1080,
    });
  });

  it("crops a wide camera frame to the visible portrait preview", () => {
    const crop = getObjectCoverSourceCrop({
      sourceWidth: 1920,
      sourceHeight: 1080,
      renderedWidth: 390,
      renderedHeight: 650,
    });

    expect(crop?.sx).toBeCloseTo(636);
    expect(crop?.sy).toBe(0);
    expect(crop?.sw).toBeCloseTo(648);
    expect(crop?.sh).toBe(1080);
  });

  it("crops a camera frame vertically for a wider rendered preview", () => {
    const crop = getObjectCoverSourceCrop({
      sourceWidth: 1920,
      sourceHeight: 1080,
      renderedWidth: 1000,
      renderedHeight: 400,
    });

    expect(crop?.sx).toBe(0);
    expect(crop?.sy).toBeCloseTo(156);
    expect(crop?.sw).toBe(1920);
    expect(crop?.sh).toBeCloseTo(768);
  });

  it("keeps the whole source frame when the rendered preview has the same aspect ratio", () => {
    expect(
      getObjectCoverSourceCrop({
        sourceWidth: 1920,
        sourceHeight: 1080,
        renderedWidth: 1280,
        renderedHeight: 720,
      }),
    ).toEqual({
      sx: 0,
      sy: 0,
      sw: 1920,
      sh: 1080,
    });
  });

  it("returns null before the source or rendered preview has dimensions", () => {
    expect(
      getObjectCoverSourceCrop({
        sourceWidth: 0,
        sourceHeight: 1080,
        renderedWidth: 390,
        renderedHeight: 650,
      }),
    ).toBeNull();
  });

  it("maps a centered rendered scan region to the covered source crop", () => {
    const crop = getObjectCoverSourceCropForRegion({
      sourceWidth: 1920,
      sourceHeight: 1080,
      renderedWidth: 600,
      renderedHeight: 600,
      renderedRegionX: 90,
      renderedRegionY: 90,
      renderedRegionWidth: 420,
      renderedRegionHeight: 420,
    });

    expect(crop?.sx).toBeCloseTo(582);
    expect(crop?.sy).toBeCloseTo(162);
    expect(crop?.sw).toBeCloseTo(756);
    expect(crop?.sh).toBeCloseTo(756);
  });

  it("clamps scan regions that extend outside the rendered preview", () => {
    const crop = getObjectCoverSourceCropForRegion({
      sourceWidth: 1000,
      sourceHeight: 1000,
      renderedWidth: 500,
      renderedHeight: 500,
      renderedRegionX: -50,
      renderedRegionY: 100,
      renderedRegionWidth: 200,
      renderedRegionHeight: 250,
    });

    expect(crop).toEqual({
      sx: 0,
      sy: 200,
      sw: 300,
      sh: 500,
    });
  });

  it("returns null when the scan region is outside the rendered preview", () => {
    expect(
      getObjectCoverSourceCropForRegion({
        sourceWidth: 1000,
        sourceHeight: 1000,
        renderedWidth: 500,
        renderedHeight: 500,
        renderedRegionX: 600,
        renderedRegionY: 100,
        renderedRegionWidth: 100,
        renderedRegionHeight: 100,
      }),
    ).toBeNull();
  });
});
