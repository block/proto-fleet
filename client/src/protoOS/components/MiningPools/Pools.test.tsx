import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Pools from "./Pools";
import { type PoolInfo } from "@/shared/types/pool";

vi.mock("@/protoOS/api", () => ({
  useTestConnection: () => ({
    pending: false,
    testConnection: vi.fn(),
  }),
}));

const pool = (priority: number, name = "", url = "", username = ""): PoolInfo => ({
  name,
  url,
  username,
  password: "",
  priority,
});

const primaryPool = pool(0, "Primary Pool", "stratum+tcp://ckpool:3333", "testuser1");
const backupPool = pool(1, "Backup Pool", "stratum+tcp://backup.example.com:3333", "testuser2");

describe("ProtoOS Pools", () => {
  it("cancels the pending edit completion when unmounted", () => {
    vi.useFakeTimers();
    try {
      const { unmount } = render(
        <Pools
          pools={[]}
          onChangePools={vi.fn()}
          renderModal={({ onDismiss }) => <button onClick={onDismiss}>Dismiss</button>}
        />,
      );
      fireEvent.click(screen.getByText("Dismiss"));
      expect(vi.getTimerCount()).toBe(1);

      unmount();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.clearAllTimers();
      vi.useRealTimers();
    }
  });

  it("uses the URL as the fallback title and renders the full username below it", () => {
    render(
      <Pools
        onChangePools={vi.fn()}
        pools={[
          {
            name: "",
            url: "stratum+tcp://mine.ocean.xyz:3334",
            username: "mann23.workerbee",
            password: "",
            priority: 0,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 1,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 2,
          },
        ]}
      />,
    );

    expect(screen.getAllByText("stratum+tcp://mine.ocean.xyz:3334")).toHaveLength(2);
    expect(screen.getByText("mann23.workerbee")).toBeInTheDocument();
  });

  it("removes a pool from the actions menu when two pools are configured", async () => {
    const onChangePools = vi.fn();
    render(<Pools onChangePools={onChangePools} pools={[primaryPool, backupPool, pool(2)]} />);

    fireEvent.click(screen.getByTestId("pool-0-actions-menu-button"));
    fireEvent.click(await screen.findByTestId("pool-0-delete-action"));

    // The second pool moves to position 0.
    await waitFor(() => {
      expect(screen.queryByTestId("pool-1-edit-button")).not.toBeInTheDocument();
      expect(screen.getByTestId("pool-0-edit-button")).toBeInTheDocument();
    });
    expect(onChangePools).toHaveBeenCalledWith(
      [expect.objectContaining({ url: backupPool.url, priority: 0 }), expect.anything(), expect.anything()],
      { isDelete: true },
    );
  });

  it("disables the delete action when one pool is configured", async () => {
    render(<Pools onChangePools={vi.fn()} pools={[primaryPool, pool(1), pool(2)]} />);

    fireEvent.click(screen.getByTestId("pool-0-actions-menu-button"));
    await screen.findByTestId("pool-0-edit-action");

    // Row renders a div, not a button, when it has no click handler.
    expect(screen.getByTestId("pool-0-delete-action").tagName).toBe("DIV");
  });

  it("saves an edit to a configured pool and closes the modal", async () => {
    const onChangePools = vi.fn();
    render(<Pools onChangePools={onChangePools} pools={[primaryPool, pool(1), pool(2)]} />);

    fireEvent.click(screen.getByTestId("pool-0-edit-button"));
    fireEvent.change(await screen.findByTestId("url-0-input"), { target: { value: "stratum+tcp://newpool:4444" } });
    fireEvent.click(screen.getByTestId("pool-save-button"));

    await waitFor(() => {
      expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();
    });
    expect(onChangePools.mock.lastCall?.[0][0]).toMatchObject({ url: "stratum+tcp://newpool:4444" });
  });

  it("does not save a pool when the modal is dismissed", async () => {
    const onChangePools = vi.fn();
    render(<Pools onChangePools={onChangePools} pools={[pool(0), pool(1), pool(2)]} />);

    expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("add-pool-button"));
    fireEvent.change(screen.getByTestId("url-0-input"), { target: { value: primaryPool.url } });
    fireEvent.click(screen.getByTestId("header-icon-button"));

    await waitFor(() => {
      expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("add-pool-button")).toBeInTheDocument();
    expect(onChangePools).not.toHaveBeenCalled();
  });
});
