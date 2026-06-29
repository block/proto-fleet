import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import General from "./General";
import { useSystemTag } from "@/protoOS/api";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AUTH_ACTIONS } from "@/protoOS/store/types";

const { mockCheckAccess, mockSetPausedAuthAction, mockState } = vi.hoisted(() => ({
  mockCheckAccess: vi.fn(),
  mockSetPausedAuthAction: vi.fn(),
  mockState: {
    hasAccess: undefined as boolean | undefined,
    pausedAuthAction: null as string | null,
    dismissedLoginModal: false,
  },
}));

// Stub the edit modal so the test can assert it opened via its `open` prop.
vi.mock("./MinerSystemTagEditModal", () => ({
  default: ({ open }: { open: boolean }) => (open ? <div data-testid="tag-edit-modal" /> : null),
}));

vi.mock("@/protoOS/features/firmwareUpdate/components/CheckForUpdate", () => ({
  default: () => null,
}));

vi.mock("@/protoOS/api", () => ({
  useSystemTag: vi.fn(),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useAccessToken: vi.fn(() => ({ checkAccess: mockCheckAccess, hasAccess: mockState.hasAccess })),
  useDismissedLoginModal: vi.fn(() => mockState.dismissedLoginModal),
  useIsProtoRig: vi.fn(() => true),
  usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
  useSetDismissedLoginModal: vi.fn(() => vi.fn()),
  useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  useSetTemperatureUnit: vi.fn(() => vi.fn()),
  useSetTheme: vi.fn(() => vi.fn()),
  useSystemInfo: vi.fn(() => ({ product_name: "Rig Alpha", os: { version: "1.2.3" } })),
  useTemperatureUnit: vi.fn(() => "C"),
  useTheme: vi.fn(() => "dark"),
}));

describe("General system-tag flow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    mockState.dismissedLoginModal = false;
    (useMinerHosting as Mock).mockReturnValue({ mode: "direct" });
    // Resolve an existing tag so the "Edit" affordance renders.
    (useSystemTag as Mock).mockReturnValue({
      getSystemTag: ({ onSuccess }: { onSuccess: (tag: string) => void }) => onSuccess("rig-01"),
    });
  });

  test("direct mode gates the edit modal behind the auth check", () => {
    const { getByTestId, queryByTestId } = render(<General />);

    fireEvent.click(getByTestId("edit-details-button"));

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.systemTag);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    expect(queryByTestId("tag-edit-modal")).not.toBeInTheDocument();
  });

  test("fleet-hosted mode opens the edit modal directly without the auth gate", () => {
    (useMinerHosting as Mock).mockReturnValue({ mode: "fleet" });

    const { getByTestId } = render(<General />);

    fireEvent.click(getByTestId("edit-details-button"));

    expect(getByTestId("tag-edit-modal")).toBeInTheDocument();
    expect(mockSetPausedAuthAction).not.toHaveBeenCalledWith(AUTH_ACTIONS.systemTag);
    expect(mockCheckAccess).not.toHaveBeenCalled();
  });
});
