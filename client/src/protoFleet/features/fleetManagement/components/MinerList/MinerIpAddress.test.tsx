import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { INACTIVE_PLACEHOLDER } from "./constants";
import MinerIpAddress from "./MinerIpAddress";
import * as storeModule from "@/protoFleet/store";

vi.mock("@/protoFleet/store");

describe("MinerIpAddress", () => {
  const deviceIdentifier = "test-device-id";

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders placeholder when IP address is not available", () => {
    vi.mocked(storeModule.useMinerIpAddress).mockReturnValue(null as any);
    vi.mocked(storeModule.useMinerUrl).mockReturnValue(null as any);

    render(<MinerIpAddress deviceIdentifier={deviceIdentifier} />);

    expect(screen.getByText(INACTIVE_PLACEHOLDER)).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders non-clickable IP when there is no URL", () => {
    vi.mocked(storeModule.useMinerIpAddress).mockReturnValue("192.168.1.100");
    vi.mocked(storeModule.useMinerUrl).mockReturnValue(null as any);

    render(<MinerIpAddress deviceIdentifier={deviceIdentifier} />);

    expect(screen.getByText("192.168.1.100")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders a link that opens in new tab for HTTP URLs", () => {
    const httpUrl = "http://192.168.1.100";
    vi.mocked(storeModule.useMinerIpAddress).mockReturnValue("192.168.1.100");
    vi.mocked(storeModule.useMinerUrl).mockReturnValue(httpUrl);

    render(<MinerIpAddress deviceIdentifier={deviceIdentifier} />);

    const link = screen.getByRole("link", { name: "192.168.1.100" });
    expect(link).toHaveAttribute("href", httpUrl);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("renders a link that opens in new tab for HTTPS URLs", () => {
    const httpsUrl = "https://192.168.1.100";
    vi.mocked(storeModule.useMinerIpAddress).mockReturnValue("192.168.1.100");
    vi.mocked(storeModule.useMinerUrl).mockReturnValue(httpsUrl);

    render(<MinerIpAddress deviceIdentifier={deviceIdentifier} />);

    const link = screen.getByRole("link", { name: "192.168.1.100" });
    expect(link).toHaveAttribute("href", httpsUrl);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });
});
