import { render, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import { navigationMenuTypes } from "./constants";
import Navigation from "./Navigation";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    Link: vi.fn(({ children }) => children),
    useLocation: () => ({
      pathname: "localhost:3000/example/path",
    }),
    useNavigate: () => ({
      Navigation: vi.fn(),
    }),
  };
});

describe("Navigation", () => {
  const macValue = "00:11:22:33:44:55";
  const versionValue = "1.2.3";

  test("renders mac info", () => {
    const { getByTestId } = render(
      <Navigation macInfo={{ loading: false, value: macValue }} type={navigationMenuTypes.app} />,
    );
    const { getByText } = within(getByTestId("mac-address-info-item"));

    // Assert that the controller MAC is rendered correctly
    expect(getByText(macValue)).toBeInTheDocument();
  });

  test("renders version info", () => {
    const { getByTestId } = render(
      <Navigation versionInfo={{ loading: false, value: versionValue }} type={navigationMenuTypes.app} />,
    );
    const { getByText } = within(getByTestId("version-info-item"));

    expect(getByText(versionValue)).toBeInTheDocument();
  });
});
