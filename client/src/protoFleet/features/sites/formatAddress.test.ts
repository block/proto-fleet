import { describe, expect, it } from "vitest";

import { formatSiteAddress } from "./formatAddress";

describe("formatSiteAddress", () => {
  it("joins address, city/state, and postal with the bullet separator", () => {
    expect(
      formatSiteAddress({
        address: "123 Main St",
        locationCity: "Chicago",
        locationState: "IL",
        postalCode: "60601",
      }),
    ).toBe("123 Main St • Chicago, IL • 60601");
  });

  it("omits empty segments so a half-filled address doesn't render stray separators", () => {
    expect(
      formatSiteAddress({
        address: "123 Main St",
        locationCity: "",
        locationState: "",
        postalCode: "60601",
      }),
    ).toBe("123 Main St • 60601");
  });

  it("trims whitespace from each field before composing", () => {
    expect(
      formatSiteAddress({
        address: "  123 Main St ",
        locationCity: "  Chicago  ",
        locationState: " IL ",
        postalCode: " 60601 ",
      }),
    ).toBe("123 Main St • Chicago, IL • 60601");
  });

  it("renders only city when state is empty", () => {
    expect(formatSiteAddress({ locationCity: "Chicago" })).toBe("Chicago");
  });

  it("appends country when includeCountry is true", () => {
    expect(
      formatSiteAddress(
        { address: "1 Bay St", locationCity: "Toronto", locationState: "ON", postalCode: "M5J 2N8", country: "CA" },
        { includeCountry: true },
      ),
    ).toBe("1 Bay St • Toronto, ON • M5J 2N8 • CA");
  });

  it("omits country by default", () => {
    expect(formatSiteAddress({ locationCity: "Toronto", country: "CA" })).toBe("Toronto");
  });

  it("returns an empty string when all parts are blank or missing", () => {
    expect(formatSiteAddress({})).toBe("");
    expect(formatSiteAddress({ address: "", locationCity: null, locationState: undefined, postalCode: "   " })).toBe(
      "",
    );
  });

  it("honors the separator option", () => {
    expect(
      formatSiteAddress(
        { address: "1 Main", locationCity: "Austin", locationState: "TX", postalCode: "78701" },
        { separator: ", " },
      ),
    ).toBe("1 Main, Austin, TX, 78701");
  });
});
