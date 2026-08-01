import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthenticatedAppContent } from "./App";

const notificationMock = vi.hoisted(() => {
  const state = {
    closeModal: vi.fn(),
    installCommand: "install v1.3.0",
    modalOpen: true,
    release: { version: "v1.3.0" },
    updatePill: { version: "v1.3.0", onClick: vi.fn() },
  };
  return {
    state,
    useUpdateNotification: vi.fn(() => state),
  };
});

vi.mock("@/protoFleet/features/updates/useUpdateNotification", () => ({
  useUpdateNotification: notificationMock.useUpdateNotification,
}));

vi.mock("@/protoFleet/components/AppLayout", () => ({
  default: ({
    children,
    hideShellHeader,
    updatePill,
  }: {
    children: ReactNode;
    hideShellHeader?: boolean;
    updatePill?: { version: string } | null;
  }) => (
    <div
      data-testid="app-layout"
      data-hide-shell-header={String(Boolean(hideShellHeader))}
      data-update-version={updatePill?.version}
    >
      {children}
    </div>
  ),
}));

vi.mock("@/protoFleet/features/updates/components/UpdateNotificationModal", () => ({
  default: ({ open, release }: { open: boolean; release?: { version: string } }) =>
    open ? <div data-testid="update-notification-modal">{release?.version}</div> : null,
}));

describe("AuthenticatedAppContent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps the global update owner and modal on fullscreen miner content", () => {
    render(
      <AuthenticatedAppContent fullscreen hideShellHeader={false}>
        <div>Miner detail</div>
      </AuthenticatedAppContent>,
    );

    expect(screen.getByText("Miner detail")).toBeInTheDocument();
    expect(screen.queryByTestId("app-layout")).not.toBeInTheDocument();
    expect(screen.getByTestId("update-notification-modal")).toHaveTextContent("v1.3.0");
    expect(notificationMock.useUpdateNotification).toHaveBeenCalledTimes(1);
  });

  it("passes the update pill and shell-header option only to normal layouts", () => {
    render(
      <AuthenticatedAppContent hideShellHeader>
        <div>Detail content</div>
      </AuthenticatedAppContent>,
    );

    expect(screen.getByTestId("app-layout")).toHaveAttribute("data-hide-shell-header", "true");
    expect(screen.getByTestId("app-layout")).toHaveAttribute("data-update-version", "v1.3.0");
    expect(screen.getByTestId("update-notification-modal")).toHaveTextContent("v1.3.0");
  });
});
