import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import FirmwareUsageBadge from "./FirmwareUsageBadge";
import { releaseChannelsApi } from "./ReleaseChannels/__tests__/helpers";
import {
  canaryChannel,
  checksums,
  completedRigRollout,
  pausedRigRollout,
  productionChannel,
} from "./ReleaseChannels/ReleaseChannels.fixtures";

const filename = "proto-rig-1.4.4.swu";
const badgeName = `View channels using ${filename}`;
const props = () => ({
  checksum: checksums.rig144,
  filename,
  api: { ...releaseChannelsApi(), channels: [canaryChannel] },
  refreshWarning: null,
  onManageChannel: vi.fn(),
});

describe("FirmwareUsageBadge", () => {
  it("opens the shared modal from the keyboard and manages the selected channel", async () => {
    const user = userEvent.setup();
    const initial = props();
    render(<FirmwareUsageBadge {...initial} />);

    await user.tab();
    expect(screen.getByRole("button", { name: badgeName })).toHaveFocus();
    expect(screen.getByRole("button", { name: badgeName })).toHaveAttribute("aria-haspopup", "dialog");
    await user.keyboard("{Enter}");
    const modal = screen.getByTestId("firmware-usage-modal");
    await waitFor(() => expect(modal).toBeVisible());
    expect(within(modal).getByText("Firmware usage")).toBeVisible();
    expect(within(modal).getByText(filename)).toBeVisible();
    expect(within(modal).getByText("Proto Rig")).toBeVisible();

    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("firmware-usage-modal")).not.toBeInTheDocument();
    screen.getByRole("button", { name: badgeName }).focus();
    await user.keyboard(" ");
    await user.click(screen.getByRole("button", { name: "Manage Canary" }));
    expect(initial.onManageChannel).toHaveBeenCalledExactlyOnceWith(canaryChannel.id);
    expect(screen.queryByTestId("firmware-usage-modal")).not.toBeInTheDocument();
  });

  it("matches the payload checksum instead of the version or uploaded file ID", async () => {
    const user = userEvent.setup();
    const initial = props();
    const api = {
      ...initial.api,
      channels: [
        {
          ...canaryChannel,
          modelGroups: canaryChannel.modelGroups.map((group) => ({
            ...group,
            firmwareFileId: "another-copy-of-this-payload",
            firmwareVersion: "1.4.4",
          })),
        },
      ],
    };
    const { rerender } = render(<FirmwareUsageBadge {...initial} api={api} checksum={checksums.rig143} />);
    expect(screen.queryByRole("button", { name: badgeName })).not.toBeInTheDocument();

    rerender(<FirmwareUsageBadge {...initial} api={api} />);
    await user.click(screen.getByRole("button", { name: badgeName }));
    await waitFor(() => expect(screen.getByTestId("firmware-usage-modal")).toBeVisible());
    expect(screen.getByRole("button", { name: "Manage Canary" })).toBeVisible();
    expect(screen.queryByText("Bitmain S21")).not.toBeInTheDocument();
  });

  it("includes assigned channels with no miners and deduplicates assignment and update references", async () => {
    const user = userEvent.setup();
    const initial = props();
    const emptyChannel = {
      ...canaryChannel,
      minerCount: 0,
      modelGroupCount: 1,
      modelGroups: [{ ...canaryChannel.modelGroups[0], minerCount: 0, onTargetCount: 0 }],
    };
    const api = { ...initial.api, channels: [emptyChannel] };
    const { rerender } = render(<FirmwareUsageBadge {...initial} api={api} />);
    await user.click(screen.getByRole("button", { name: badgeName }));
    await waitFor(() => expect(screen.getByTestId("firmware-usage-modal")).toBeVisible());
    const modal = within(screen.getByTestId("firmware-usage-modal"));
    expect(modal.getByRole("button", { name: "Manage Canary" })).toBeVisible();

    rerender(<FirmwareUsageBadge {...initial} api={{ ...api, rollouts: [pausedRigRollout] }} />);
    expect(modal.getAllByRole("listitem")).toHaveLength(1);
    expect(modal.getByText("Proto Rig")).toBeVisible();
    expect(modal.getAllByRole("button", { name: "Manage Canary" })).toHaveLength(1);
  });

  it("includes a paused active update without an assignment but not completed history", async () => {
    const user = userEvent.setup();
    const initial = props();
    const api = {
      ...initial.api,
      channels: [{ ...canaryChannel, modelGroups: [] }],
      rollouts: [pausedRigRollout],
    };
    const { rerender } = render(<FirmwareUsageBadge {...initial} api={api} />);
    await user.click(screen.getByRole("button", { name: badgeName }));
    await waitFor(() => expect(screen.getByTestId("firmware-usage-modal")).toBeVisible());
    expect(screen.getByRole("button", { name: "Manage Canary" })).toBeVisible();

    rerender(
      <FirmwareUsageBadge
        {...initial}
        api={{ ...api, rollouts: [{ ...completedRigRollout, firmwareChecksum: checksums.rig144 }] }}
      />,
    );
    expect(screen.queryByRole("button", { name: badgeName })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Manage Canary" })).not.toBeInTheDocument();
    expect(screen.getByText("No release channels currently use this firmware.")).toBeVisible();
  });

  it("updates channel references while the modal is open and keeps Manage routed to the current channel", async () => {
    const user = userEvent.setup();
    const initial = props();
    const production = { ...productionChannel, modelGroups: [canaryChannel.modelGroups[0]] };
    const { rerender } = render(<FirmwareUsageBadge {...initial} />);
    await user.click(screen.getByRole("button", { name: badgeName }));
    await waitFor(() => expect(screen.getByTestId("firmware-usage-modal")).toBeVisible());

    rerender(<FirmwareUsageBadge {...initial} api={{ ...initial.api, channels: [canaryChannel, production] }} />);
    expect(within(screen.getByTestId("firmware-usage-modal")).getAllByRole("listitem")).toHaveLength(2);
    rerender(<FirmwareUsageBadge {...initial} api={{ ...initial.api, channels: [production] }} />);
    expect(screen.queryByRole("button", { name: "Manage Canary" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: `Manage ${production.name}` }));
    expect(initial.onManageChannel).toHaveBeenCalledExactlyOnceWith(production.id);
    expect(screen.queryByTestId("firmware-usage-modal")).not.toBeInTheDocument();
  });

  it("waits for the initial read and preserves the warning alongside retained references after a refresh error", async () => {
    const user = userEvent.setup();
    const initial = props();
    const { rerender } = render(<FirmwareUsageBadge {...initial} api={{ ...initial.api, hasLoaded: false }} />);
    expect(screen.queryByRole("button", { name: badgeName })).not.toBeInTheDocument();

    const error = new Error("Couldn't refresh release channels");
    const refreshWarning = <p role="alert">{error.message}</p>;
    rerender(<FirmwareUsageBadge {...initial} api={{ ...initial.api, error }} refreshWarning={refreshWarning} />);
    await user.click(screen.getByRole("button", { name: badgeName }));
    await waitFor(() => expect(screen.getByTestId("firmware-usage-modal")).toBeVisible());
    expect(screen.getByRole("alert")).toHaveTextContent(error.message);
    expect(screen.getByRole("button", { name: "Manage Canary" })).toBeVisible();

    rerender(
      <FirmwareUsageBadge {...initial} api={{ ...initial.api, channels: [], error }} refreshWarning={refreshWarning} />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(error.message);
    expect(screen.queryByText("No release channels currently use this firmware.")).not.toBeInTheDocument();
  });
});
