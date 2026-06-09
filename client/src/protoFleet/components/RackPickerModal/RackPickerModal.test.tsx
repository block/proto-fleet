import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { DeviceSetSchema, DeviceSetType, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const mockListRacks = vi.fn();
// Stable mock reference so the component's useEffect deps stay
// unchanged across re-renders — otherwise the effect would re-fire on
// every render and the test would spin forever.
const stableDeviceSets = { listRacks: mockListRacks };

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => stableDeviceSets,
}));

// eslint-disable-next-line import-x/order -- import must come after vi.mock calls
import RackPickerModal from "./RackPickerModal";

const makeRack = (id: number, label: string, buildingId?: number) =>
  create(DeviceSetSchema, {
    id: BigInt(id),
    label,
    type: DeviceSetType.RACK,
    deviceCount: 0,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, {
        rows: 0,
        columns: 0,
        zone: "",
        buildingId: buildingId !== undefined ? BigInt(buildingId) : undefined,
      }),
    },
  });

beforeEach(() => {
  vi.clearAllMocks();
  mockListRacks.mockImplementation(({ onSuccess }: { onSuccess: (rows: unknown[]) => void }) => {
    onSuccess([makeRack(1, "R1"), makeRack(2, "R2", 7), makeRack(3, "R3")]);
    return Promise.resolve();
  });
});

describe("RackPickerModal", () => {
  it("renders all racks when no exclude filter is supplied", async () => {
    render(<RackPickerModal show title="Pick racks" onDismiss={vi.fn()} onConfirm={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText("R1")).toBeInTheDocument();
      expect(screen.getByText("R2")).toBeInTheDocument();
      expect(screen.getByText("R3")).toBeInTheDocument();
    });
  });

  it("hides racks already in the excluded building", async () => {
    render(<RackPickerModal show title="Pick racks" excludeBuildingId={7n} onDismiss={vi.fn()} onConfirm={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText("R1")).toBeInTheDocument();
      expect(screen.getByText("R3")).toBeInTheDocument();
    });
    expect(screen.queryByText("R2")).not.toBeInTheDocument();
  });

  it("surfaces a load error when listRacks fails", async () => {
    mockListRacks.mockImplementation(({ onError }: { onError: (message: string) => void }) => {
      onError("network is down");
      return Promise.resolve();
    });
    render(<RackPickerModal show title="Pick racks" onDismiss={vi.fn()} onConfirm={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText("network is down")).toBeInTheDocument();
    });
  });

  it("returns nothing when show is false", () => {
    render(<RackPickerModal show={false} title="Pick racks" onDismiss={vi.fn()} onConfirm={vi.fn()} />);
    expect(screen.queryByText("Pick racks")).not.toBeInTheDocument();
    expect(mockListRacks).not.toHaveBeenCalled();
  });
});
