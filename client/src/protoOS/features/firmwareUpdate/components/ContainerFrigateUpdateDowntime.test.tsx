import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ContainerFrigateUpdateDowntime from "./ContainerFrigateUpdateDowntime";

const mocks = vi.hoisted(() => ({ useSystemInfo: vi.fn(), useMinerHosting: vi.fn() }));

vi.mock("@/protoOS/store", () => ({ useSystemInfo: mocks.useSystemInfo }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: mocks.useMinerHosting }));
vi.mock("./ContainerFrigateSoftwareUpdate", () => ({
  default: (props: { deviceId: string; duringDowntime?: boolean }) => (
    <div>
      update for {props.deviceId} {props.duringDowntime ? "during downtime" : ""}
    </div>
  ),
}));

const container = { identity: { deviceType: "container", deviceId: "device" } };
const hosting = { adapter: { frigateServerUpdate: {} }, isFleetHosted: false };

describe("ContainerFrigateUpdateDowntime", () => {
  beforeEach(() => {
    mocks.useSystemInfo.mockReturnValue(container);
    mocks.useMinerHosting.mockReturnValue(hosting);
  });

  it("shows the update for a container reached directly", () => {
    render(<ContainerFrigateUpdateDowntime />);
    expect(screen.getByText("update for device during downtime")).toBeInTheDocument();
  });

  it.each([
    ["Fleet-hosted", container, { ...hosting, isFleetHosted: true }],
    ["not a container", { identity: { deviceType: "miner", deviceId: "device" } }, hosting],
    ["without an update client", container, { adapter: {}, isFleetHosted: false }],
    ["before the identity has loaded", undefined, hosting],
    ["without a device id", { identity: { deviceType: "container" } }, hosting],
  ])("shows nothing when %s", (_, systemInfo, minerHosting) => {
    mocks.useSystemInfo.mockReturnValue(systemInfo);
    mocks.useMinerHosting.mockReturnValue(minerHosting);
    const { container: root } = render(<ContainerFrigateUpdateDowntime />);
    expect(root).toBeEmptyDOMElement();
  });
});
