import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import GroupSelectionModal from "./GroupSelectionModal";
import { DeviceSetSchema, GroupInfoSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

const { listGroupsMock, pushToastMock } = vi.hoisted(() => ({
  listGroupsMock: vi.fn(),
  pushToastMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listGroups: listGroupsMock,
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
    buttons?: Array<{ text: string; onClick?: () => void; disabled?: boolean }>;
    title?: string;
  }) => (
    <div>
      <div>{title}</div>
      {children}
      {buttons?.map((button) => (
        <button key={button.text} type="button" onClick={button.onClick} disabled={button.disabled}>
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

const createGroup = (id: bigint, label: string, deviceCount = 1) =>
  create(DeviceSetSchema, {
    id,
    label,
    deviceCount,
    typeDetails: {
      case: "groupInfo",
      value: create(GroupInfoSchema, {}),
    },
  });

type ListGroupsCallbacks = {
  onSuccess?: (deviceSets: ReturnType<typeof createGroup>[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
};

describe("GroupSelectionModal", () => {
  beforeEach(() => {
    listGroupsMock.mockReset();
    pushToastMock.mockReset();
  });

  it("drops deleted group targets before saving", async () => {
    listGroupsMock.mockImplementation(({ onSuccess, onFinally }: ListGroupsCallbacks) => {
      onSuccess?.([createGroup(1n, "Group 1")]);
      onFinally?.();
    });

    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<GroupSelectionModal open selectedGroupIds={["1", "deleted-group"]} onDismiss={vi.fn()} onSave={onSave} />);

    await waitFor(() => expect(screen.getByText("Group 1")).toBeVisible());
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(onSave).toHaveBeenCalledWith(["1"]);
  });

  it("pushes an error toast when the group list fails to load", async () => {
    listGroupsMock.mockImplementation(({ onError, onFinally }: ListGroupsCallbacks) => {
      onError?.("boom");
      onFinally?.();
    });

    render(<GroupSelectionModal open selectedGroupIds={[]} onDismiss={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() =>
      expect(pushToastMock).toHaveBeenCalledWith(expect.objectContaining({ message: "boom", status: "error" })),
    );
  });

  it("disables Done and never calls onSave when the group list fails to load", async () => {
    listGroupsMock.mockImplementation(({ onError, onFinally }: ListGroupsCallbacks) => {
      onError?.("boom");
      onFinally?.();
    });

    const onSave = vi.fn();
    const user = userEvent.setup();

    render(<GroupSelectionModal open selectedGroupIds={["stale-1", "stale-2"]} onDismiss={vi.fn()} onSave={onSave} />);

    const doneButton = await screen.findByRole("button", { name: "Done" });
    await waitFor(() => expect(doneButton).toBeDisabled());
    await user.click(doneButton);

    expect(onSave).not.toHaveBeenCalled();
  });
});
