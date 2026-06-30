import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import PageHeader from "./PageHeader";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: vi.fn(),
}));

vi.mock("./GlobalActions", () => ({
  default: () => <div data-testid="global-actions" />,
}));

vi.mock("./MinerStatus", () => ({
  default: () => <div data-testid="miner-status" />,
}));

vi.mock("./PoolStatus", () => ({
  default: () => <div data-testid="pool-status" />,
}));

vi.mock("./Power", () => ({
  default: () => <div data-testid="power-widget" />,
}));

vi.mock("./PowerTarget", () => ({
  default: () => <div data-testid="power-target" />,
}));

vi.mock("@/protoOS/features/firmwareUpdate/components/FirmwareUpdateStatus", () => ({
  default: () => <div data-testid="firmware-update-status" />,
}));

describe("PageHeader", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useWindowDimensions).mockReturnValue({
      isDesktop: true,
      isLaptop: false,
      isPhone: false,
      isTablet: false,
      width: 1440,
      height: 900,
    });
  });

  test("renders miner action widgets", () => {
    render(<PageHeader title="Cooling" />);

    expect(screen.getByTestId("power-target")).toBeInTheDocument();
    expect(screen.getByTestId("pool-status")).toBeInTheDocument();
    expect(screen.getByTestId("power-widget")).toBeInTheDocument();
    expect(screen.getByTestId("global-actions")).toBeInTheDocument();
  });
});
