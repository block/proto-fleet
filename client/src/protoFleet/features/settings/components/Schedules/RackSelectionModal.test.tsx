import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import RackSelectionModal from "./RackSelectionModal";
import { DeviceSetSchema, RackInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const { listRacksMock, pushToastMock } = vi.hoisted(() => ({
  listRacksMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listRacks: listRacksMock,
  }),
}));

vi.mock("@/shared/components/Modal", () => ({
  __esModule: true,
  default: ({
    children,
    buttons,
    title,
  }: {
    children: ReactNode;
    buttons?: Array<{ text: string; onClick?: () => void }>;
    title?: string;
  }) => (
    <div>
      <div>{title}</div>
      {children}
      {buttons?.map((button) => (
        <button key={button.text} type="button" onClick={button.onClick}>
          {button.text}
        </button>
      ))}
    </div>
  ),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: (...args: unknown[]) => pushToastMock(...args),
  STATUSES: {
    error: "error",
  },
}));

const createRack = (id: bigint, label: string) =>
  create(DeviceSetSchema, {
    id,
    label,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, {
        rows: 1,
        columns: 1,
        zone: "Zone A",
      }),
    },
  });

type ListRacksCallbacks = {
  onSuccess?: (deviceSets: ReturnType<typeof createRack>[]) => void;
  onFinally?: () => void;
};

describe("RackSelectionModal", () => {
  beforeEach(() => {
    listRacksMock.mockReset();
    pushToastMock.mockReset();
  });

  it("drops deleted rack targets before saving", async () => {
    listRacksMock.mockImplementation(({ onSuccess, onFinally }: ListRacksCallbacks) => {
      onSuccess?.([createRack(1n, "Rack 1")]);
      onFinally?.();
    });

    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<RackSelectionModal open selectedRackIds={["1", "deleted-rack"]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Rack 1")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(listRacksMock).toHaveBeenCalledWith(expect.not.objectContaining({ pageSize: expect.anything() }));
    expect(onSave).toHaveBeenCalledWith(["1"]);
  });
});
