import { useLocation } from "react-router-dom";
import { render, within } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import { navigationMenuTypes } from "./constants";
import Navigation from "./Navigation";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDeviceType } from "@/protoOS/store";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    Link: vi.fn(({ children, to }) => <a href={to}>{children}</a>),
    useLocation: vi.fn(),
    useNavigate: () => ({
      Navigation: vi.fn(),
    }),
  };
});

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: vi.fn() }));

vi.mock("@/protoOS/store", () => ({
  useDeviceType: vi.fn(),
}));

describe("Navigation", () => {
  const macValue = "00:11:22:33:44:55";
  const versionValue = "1.2.3";

  beforeEach(() => {
    vi.clearAllMocks();
    (useDeviceType as Mock).mockReturnValue("rig");
    (useLocation as Mock).mockReturnValue({ pathname: "/example/path" });
    (useMinerHosting as Mock).mockReturnValue({ minerRoot: "", isFleetHosted: false });
  });

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

  test("uses the container index route for the app logo in container mode", () => {
    (useDeviceType as Mock).mockReturnValue("container");

    const { container } = render(<Navigation type={navigationMenuTypes.app} />);

    expect(container.querySelector('a[href="/"]')).toBeInTheDocument();
  });

  test.each(["", "hashrate", "efficiency", "power-usage", "temperature"])(
    "highlights Home on container route /%s",
    (route) => {
      (useDeviceType as Mock).mockReturnValue("container");
      (useLocation as Mock).mockReturnValue({ pathname: `/${route}` });
      const { getByRole } = render(<Navigation type={navigationMenuTypes.app} />);
      expect(getByRole("button", { name: "Home" })).toHaveAttribute("aria-current", "page");
      expect(getByRole("button", { name: "Home" })).toHaveClass("bg-core-primary-5");
      expect(getByRole("button", { name: "Settings" })).not.toHaveAttribute("aria-current");
    },
  );

  test.each(["/miners/ct1", "/miners/ct1/", "/miners/ct1/power-usage"])(
    "highlights Home for Fleet-hosted route %s",
    (pathname) => {
      (useDeviceType as Mock).mockReturnValue("container");
      (useLocation as Mock).mockReturnValue({ pathname });
      (useMinerHosting as Mock).mockReturnValue({ minerRoot: "/miners/ct1", isFleetHosted: true });
      const { getByRole } = render(<Navigation type={navigationMenuTypes.app} />);
      expect(getByRole("button", { name: "Home" })).toHaveAttribute("aria-current", "page");
    },
  );

  test("selects the current destination instead of Home outside the Home routes", () => {
    (useDeviceType as Mock).mockReturnValue("container");
    (useLocation as Mock).mockReturnValue({ pathname: "/tanks" });
    const { getByRole } = render(<Navigation type={navigationMenuTypes.app} />);
    expect(getByRole("button", { name: "Home" })).not.toHaveAttribute("aria-current");
    expect(getByRole("button", { name: "Tanks" })).toHaveAttribute("aria-current", "page");
  });
});
