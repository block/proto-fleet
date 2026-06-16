import { render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import LocationSelector from "./LocationSelector";

describe("Location Selector", () => {
  const skeletonTestId = "skeleton-bar";

  const locationName = "Test lab";

  test("renders loading state", () => {
    const { getByTestId } = render(<LocationSelector loading={true} location={undefined} />);

    expect(getByTestId(skeletonTestId)).toBeDefined();
  });

  test("renders location name", () => {
    const { getByText, queryByTestId } = render(<LocationSelector loading={false} location={locationName} />);

    expect(queryByTestId(skeletonTestId)).toBeNull();
    expect(getByText(locationName)).toHaveClass("min-w-0", "truncate");
  });
});
