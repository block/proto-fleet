import { describe, expect, it } from "vitest";
import { firmwareVersionFromFilename } from "./firmwareVersion";

describe("firmwareVersionFromFilename", () => {
  it.each([
    ["miner-image-release-c3-p1-1.3.5.swu", "1.3.5"],
    ["antminer-s19-v2.1.0.tar.gz", "2.1.0"],
    ["3.4.5", "3.4.5"],
  ])("reads %s as version %s", (filename, expected) => {
    expect(firmwareVersionFromFilename(filename)).toBe(expected);
  });

  it.each(["antminer-firmware.tar.gz", "antminer-2.1.tar.gz", "build-1.2.3.4.swu"])(
    "does not infer a version from %s",
    (filename) => {
      expect(firmwareVersionFromFilename(filename)).toBeNull();
    },
  );
});
