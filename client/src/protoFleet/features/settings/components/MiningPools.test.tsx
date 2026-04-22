import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MiningPools from "./MiningPools";
import type { Pool } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/protoFleet/api/usePools", () => ({
  default: vi.fn(() => ({
    pools: [],
    miningPools: [],
    createPool: vi.fn(),
    updatePool: vi.fn(),
    deletePool: vi.fn(),
    validatePool: vi.fn(),
    validatePoolPending: false,
    isLoading: false,
  })),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    success: "success",
    error: "error",
  },
}));

vi.mock("@bufbuild/protobuf", () => ({
  create: vi.fn((_schema, data) => data),
}));

vi.mock("@/protoFleet/api/generated/pools/v1/pools_pb", () => ({
  CreatePoolRequestSchema: {},
  UpdatePoolRequestSchema: {},
}));

beforeEach(() => {
  vi.clearAllMocks();
});

describe("MiningPools", () => {
  describe("Loading state", () => {
    it("displays loading spinner when isLoading is true", async () => {
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValueOnce({
        pools: [],
        miningPools: [],
        createPool: vi.fn(),
        updatePool: vi.fn(),
        deletePool: vi.fn(),
        validatePool: vi.fn(),
        validatePoolPending: false,
        isLoading: true,
      });

      render(<MiningPools />);

      // Should show loading spinner (SVG with animate-spin class)
      const spinner = document.querySelector(".animate-spin");
      expect(spinner).toBeInTheDocument();

      // Should not show empty state or table content
      expect(screen.queryByText(/No pools yet/)).not.toBeInTheDocument();
    });
  });

  describe("Empty state", () => {
    it("renders empty state when no pools exist", () => {
      render(<MiningPools />);

      expect(screen.getAllByText("Pools").length).toBeGreaterThan(0);
      expect(screen.getByText(/No pools yet/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /add pool/i })).toBeInTheDocument();
    });

    it("shows add pool button in empty state", () => {
      render(<MiningPools />);

      const addButton = screen.getByRole("button", { name: /add pool/i });
      expect(addButton).toBeInTheDocument();
    });
  });

  describe("Table view", () => {
    const mockPools = [
      {
        poolId: BigInt(1),
        poolName: "Test Pool 1",
        url: "stratum+tcp://test1.com:3333",
        username: "user1",
      },
      {
        poolId: BigInt(2),
        poolName: "Test Pool 2",
        url: "stratum+tcp://test2.com:3334",
        username: "user2",
      },
    ] as Pool[];

    beforeEach(async () => {
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValue({
        pools: mockPools,
        miningPools: [],
        createPool: vi.fn(),
        updatePool: vi.fn(),
        deletePool: vi.fn(),
        validatePool: vi.fn(),
        validatePoolPending: false,
        isLoading: false,
      });
    });

    it("renders table with pools", () => {
      render(<MiningPools />);

      expect(screen.getAllByText("Pools").length).toBeGreaterThan(0);
      expect(screen.getAllByText("Add and manage the pools for your fleet.").length).toBeGreaterThan(0);

      // Table headers (duplicated for desktop and mobile views)
      expect(screen.getAllByText("Name").length).toBeGreaterThan(0);
      expect(screen.getAllByText("URL").length).toBeGreaterThan(0);
      expect(screen.getAllByText("Username").length).toBeGreaterThan(0);

      // Pool data (duplicated for desktop and mobile views)
      expect(screen.getAllByText("Test Pool 1").length).toBeGreaterThan(0);
      // URLs now contain zero-width spaces for better wrapping, so we use a text matcher function
      expect(
        screen.getAllByText((_content, element) => {
          const text = element?.textContent?.replace(/\u200B/g, "") || "";
          return text === "stratum+tcp://test1.com:3333";
        }).length,
      ).toBeGreaterThan(0);
      // Usernames also contain zero-width spaces for better wrapping
      expect(
        screen.getAllByText((_content, element) => {
          const text = element?.textContent?.replace(/\u200B/g, "") || "";
          return text === "user1";
        }).length,
      ).toBeGreaterThan(0);

      expect(screen.getAllByText("Test Pool 2").length).toBeGreaterThan(0);
      expect(
        screen.getAllByText((_content, element) => {
          const text = element?.textContent?.replace(/\u200B/g, "") || "";
          return text === "stratum+tcp://test2.com:3334";
        }).length,
      ).toBeGreaterThan(0);
      expect(
        screen.getAllByText((_content, element) => {
          const text = element?.textContent?.replace(/\u200B/g, "") || "";
          return text === "user2";
        }).length,
      ).toBeGreaterThan(0);
    });

    it("shows add pool button", () => {
      render(<MiningPools />);

      const addButtons = screen.getAllByRole("button", { name: /add pool/i });
      expect(addButtons.length).toBeGreaterThan(0);
    });
  });

  describe("Test connection", () => {
    const mockPool = {
      poolId: BigInt(1),
      poolName: "Test Pool",
      url: "stratum+tcp://test.com:3333",
      username: "testuser",
    } as Pool;

    let mockValidatePool: ReturnType<typeof vi.fn>;

    beforeEach(async () => {
      mockValidatePool = vi.fn();
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValue({
        pools: [mockPool],
        miningPools: [],
        createPool: vi.fn(),
        updatePool: vi.fn(),
        deletePool: vi.fn(),
        validatePool: mockValidatePool as any,
        validatePoolPending: false,
        isLoading: false,
      });
    });

    it("calls validatePool when clicking test connection", async () => {
      render(<MiningPools />);

      // Open menu - find options button by its aria-label
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);

      // Click "Test connection"
      const testButton = screen.getByText("Test connection");
      fireEvent.click(testButton);

      expect(mockValidatePool).toHaveBeenCalledWith(
        expect.objectContaining({
          poolInfo: {
            url: "stratum+tcp://test.com:3333",
            username: "testuser",
            password: "",
          },
        }),
      );
    });

    it("shows success toast when connection test passes", async () => {
      mockValidatePool.mockImplementation(({ onSuccess }) => {
        onSuccess?.();
      });

      render(<MiningPools />);

      // Open menu and click test connection
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);
      const testButton = screen.getByText("Test connection");
      fireEvent.click(testButton);

      await waitFor(() => {
        expect(pushToast).toHaveBeenCalledWith({
          message: "Pool connection successful",
          status: "success",
        });
      });
    });

    it("shows connection failed message in table when test fails", async () => {
      mockValidatePool.mockImplementation(({ onError }) => {
        onError?.();
      });

      render(<MiningPools />);

      // Open menu and click test connection
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);
      const testButton = screen.getByText("Test connection");
      fireEvent.click(testButton);

      await waitFor(() => {
        expect(screen.getAllByText("Connection failed").length).toBeGreaterThan(0);
      });
    });
  });

  describe("Create pool", () => {
    let mockCreatePool: ReturnType<typeof vi.fn>;

    beforeEach(async () => {
      mockCreatePool = vi.fn();
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValue({
        pools: [],
        miningPools: [],
        createPool: mockCreatePool as any,
        updatePool: vi.fn(),
        deletePool: vi.fn(),
        validatePool: vi.fn(),
        validatePoolPending: false,
        isLoading: false,
      });
    });

    it("opens add pool modal when clicking add pool button", () => {
      render(<MiningPools />);

      const addButton = screen.getByRole("button", { name: /add pool/i });
      fireEvent.click(addButton);

      // Modal should be visible with empty input fields (add mode)
      expect(screen.getByText("Hashrate contributes to default mining pools.")).toBeInTheDocument();
      const urlInput = screen.getByTestId("url-0-input") as HTMLInputElement;
      const usernameInput = screen.getByTestId("username-0-input") as HTMLInputElement;
      expect(urlInput.value).toBe("");
      expect(usernameInput.value).toBe("");
      expect(screen.getByTestId("modal").textContent).toContain(
        "Worker name will be appended to this username when applied to miners.",
      );
    });

    it("calls createPool with correct data when submitting new pool", async () => {
      mockCreatePool.mockImplementation(({ onSuccess }) => {
        onSuccess?.();
        return Promise.resolve();
      });

      render(<MiningPools />);

      // Open modal
      const addButton = screen.getByRole("button", { name: /add pool/i });
      fireEvent.click(addButton);

      // Fill in pool details using test IDs
      const nameInput = screen.getByTestId("pool-name-0-input");
      const urlInput = screen.getByTestId("url-0-input");
      const usernameInput = screen.getByTestId("username-0-input");
      const passwordInput = screen.getByTestId("password-0-input");

      fireEvent.change(nameInput, { target: { value: "New Pool" } });
      fireEvent.blur(nameInput);
      fireEvent.change(urlInput, { target: { value: "stratum+tcp://newpool.com:3333" } });
      fireEvent.blur(urlInput);
      fireEvent.change(usernameInput, { target: { value: "newuser" } });
      fireEvent.blur(usernameInput);
      fireEvent.change(passwordInput, { target: { value: "newpassword" } });
      fireEvent.blur(passwordInput);

      // Submit form
      const saveButton = screen.getByTestId("pool-save-button");
      fireEvent.click(saveButton);

      await waitFor(() => {
        expect(mockCreatePool).toHaveBeenCalledWith(
          expect.objectContaining({
            createPoolRequest: expect.objectContaining({
              poolConfig: expect.objectContaining({
                url: "stratum+tcp://newpool.com:3333",
                username: "newuser",
                password: "newpassword",
              }),
            }),
          }),
        );
      });
    });

    it("shows success toast when pool is created successfully", async () => {
      mockCreatePool.mockImplementation(({ onSuccess }) => {
        onSuccess?.();
        return Promise.resolve();
      });

      render(<MiningPools />);

      // Open modal and submit
      const addButton = screen.getByRole("button", { name: /add pool/i });
      fireEvent.click(addButton);

      const nameInput = screen.getByTestId("pool-name-0-input");
      fireEvent.change(nameInput, { target: { value: "Test Pool" } });
      fireEvent.blur(nameInput);

      const urlInput = screen.getByTestId("url-0-input");
      fireEvent.change(urlInput, { target: { value: "stratum+tcp://test.com:3333" } });
      fireEvent.blur(urlInput);

      const usernameInput = screen.getByTestId("username-0-input");
      fireEvent.change(usernameInput, { target: { value: "testuser" } });
      fireEvent.blur(usernameInput);

      const saveButton = screen.getByTestId("pool-save-button");
      fireEvent.click(saveButton);

      await waitFor(() => {
        expect(pushToast).toHaveBeenCalledWith({
          message: "Pool added",
          status: "success",
        });
      });
    });

    it("rejects usernames with workername separators before creating a pool", async () => {
      render(<MiningPools />);

      const addButton = screen.getByRole("button", { name: /add pool/i });
      fireEvent.click(addButton);

      fireEvent.change(screen.getByTestId("pool-name-0-input"), { target: { value: "Test Pool" } });
      fireEvent.change(screen.getByTestId("url-0-input"), { target: { value: "stratum+tcp://test.com:3333" } });
      fireEvent.change(screen.getByTestId("username-0-input"), { target: { value: "wallet.worker01" } });

      fireEvent.click(screen.getByTestId("pool-save-button"));

      expect(mockCreatePool).not.toHaveBeenCalled();
      expect(
        screen.getByText(
          "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
        ),
      ).toBeInTheDocument();
    });
  });

  describe("Clickable pool rows", () => {
    const mockPool = {
      poolId: BigInt(1),
      poolName: "Clickable Pool",
      url: "stratum+tcp://clickable.com:3333",
      username: "clickuser",
    } as Pool;

    beforeEach(async () => {
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValue({
        pools: [mockPool],
        miningPools: [],
        createPool: vi.fn(),
        updatePool: vi.fn(),
        deletePool: vi.fn(),
        validatePool: vi.fn(),
        validatePoolPending: false,
        isLoading: false,
      });
    });

    it("pool rows do not have role='button' to avoid a11y conflict with nested controls", () => {
      render(<MiningPools />);

      const poolRows = screen.getAllByTestId("pool-row");
      for (const row of poolRows) {
        expect(row).not.toHaveAttribute("role", "button");
        expect(row).toHaveAttribute("tabindex", "0");
      }
    });

    it("clicking a pool row opens the edit modal", async () => {
      render(<MiningPools />);

      const poolRow = screen.getAllByTestId("pool-row")[0];
      fireEvent.click(poolRow);

      await waitFor(() => {
        expect(screen.getByText("Hashrate contributes to default mining pools.")).toBeInTheDocument();
        const urlInput = screen.getByTestId("url-0-input") as HTMLInputElement;
        const usernameInput = screen.getByTestId("username-0-input") as HTMLInputElement;
        expect(urlInput.value).toBe("stratum+tcp://clickable.com:3333");
        expect(usernameInput.value).toBe("clickuser");
      });
    });

    it("clicking the ellipsis menu button does not trigger the row click", async () => {
      render(<MiningPools />);

      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);

      // The menu should open (shows "Edit pool" option)
      expect(screen.getByText("Edit pool")).toBeInTheDocument();

      // The edit modal should NOT have opened (no modal inputs visible)
      expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();
    });

    it("pressing Enter on a focused pool row opens the edit modal", async () => {
      render(<MiningPools />);

      const poolRow = screen.getAllByTestId("pool-row")[0];
      fireEvent.keyDown(poolRow, { key: "Enter" });

      await waitFor(() => {
        expect(screen.getByText("Hashrate contributes to default mining pools.")).toBeInTheDocument();
        const urlInput = screen.getByTestId("url-0-input") as HTMLInputElement;
        expect(urlInput.value).toBe("stratum+tcp://clickable.com:3333");
      });
    });

    it("pressing Space on a focused pool row opens the edit modal", async () => {
      render(<MiningPools />);

      const poolRow = screen.getAllByTestId("pool-row")[0];
      fireEvent.keyDown(poolRow, { key: " " });

      await waitFor(() => {
        expect(screen.getByText("Hashrate contributes to default mining pools.")).toBeInTheDocument();
        const urlInput = screen.getByTestId("url-0-input") as HTMLInputElement;
        expect(urlInput.value).toBe("stratum+tcp://clickable.com:3333");
      });
    });

    it("clicking a menu item does not also open the edit modal", async () => {
      render(<MiningPools />);

      // Open the options menu
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);

      // Click "Test connection" menu item
      const testButton = screen.getByText("Test connection");
      fireEvent.click(testButton);

      // The edit modal should NOT have opened
      expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();
    });

    it("pressing Enter on the focused ellipsis button does not open the edit modal", async () => {
      render(<MiningPools />);

      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.keyDown(optionsButton, { key: "Enter" });

      // The edit modal should NOT have opened
      expect(screen.queryByTestId("url-0-input")).not.toBeInTheDocument();
    });
  });

  describe("Edit pool", () => {
    const mockPool = {
      poolId: BigInt(1),
      poolName: "Existing Pool",
      url: "stratum+tcp://existing.com:3333",
      username: "existinguser",
    } as Pool;

    let mockUpdatePool: ReturnType<typeof vi.fn>;

    beforeEach(async () => {
      mockUpdatePool = vi.fn();
      const usePools = (await import("@/protoFleet/api/usePools")).default;
      vi.mocked(usePools).mockReturnValue({
        pools: [mockPool],
        miningPools: [],
        createPool: vi.fn(),
        updatePool: mockUpdatePool as any,
        deletePool: vi.fn(),
        validatePool: vi.fn(),
        validatePoolPending: false,
        isLoading: false,
      });
    });

    it("opens edit pool modal when clicking edit pool from menu", async () => {
      render(<MiningPools />);

      // Open menu
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);

      // Click "Edit pool"
      const editButton = screen.getByText("Edit pool");
      fireEvent.click(editButton);

      // Modal should open with pre-filled data (edit mode)
      await waitFor(() => {
        expect(screen.getByText("Hashrate contributes to default mining pools.")).toBeInTheDocument();
        const urlInput = screen.getByTestId("url-0-input") as HTMLInputElement;
        const usernameInput = screen.getByTestId("username-0-input") as HTMLInputElement;
        expect(urlInput.value).toBe("stratum+tcp://existing.com:3333");
        expect(usernameInput.value).toBe("existinguser");
      });
    });

    it("pre-fills modal with existing pool data", async () => {
      render(<MiningPools />);

      // Open menu and click edit
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);
      const editButton = screen.getByText("Edit pool");
      fireEvent.click(editButton);

      // Verify pre-filled data
      await waitFor(() => {
        const urlInput = screen.getByDisplayValue("stratum+tcp://existing.com:3333");
        const usernameInput = screen.getByDisplayValue("existinguser");
        expect(urlInput).toBeInTheDocument();
        expect(usernameInput).toBeInTheDocument();
      });
    });

    it("calls updatePool with correct data when saving changes", async () => {
      mockUpdatePool.mockImplementation(({ onSuccess }) => {
        onSuccess?.();
        return Promise.resolve();
      });

      render(<MiningPools />);

      // Open edit modal
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);
      const editButton = screen.getByText("Edit pool");
      fireEvent.click(editButton);

      // Update pool data
      await waitFor(() => {
        const urlInput = screen.getByTestId("url-0-input");
        fireEvent.change(urlInput, { target: { value: "stratum+tcp://updated.com:4444" } });
        fireEvent.blur(urlInput);
      });

      const saveButton = screen.getByTestId("pool-save-button");
      fireEvent.click(saveButton);

      await waitFor(() => {
        expect(mockUpdatePool).toHaveBeenCalledWith(
          expect.objectContaining({
            updatePoolRequest: expect.objectContaining({
              poolId: BigInt(1),
              url: "stratum+tcp://updated.com:4444",
            }),
          }),
        );
      });
    });

    it("shows success toast when pool is updated successfully", async () => {
      mockUpdatePool.mockImplementation(({ onSuccess }) => {
        onSuccess?.();
        return Promise.resolve();
      });

      render(<MiningPools />);

      // Open edit modal and save
      const optionsButton = screen.getByRole("button", { name: "Options menu" });
      fireEvent.click(optionsButton);
      const editButton = screen.getByText("Edit pool");
      fireEvent.click(editButton);

      await waitFor(() => {
        const saveButton = screen.getByTestId("pool-save-button");
        fireEvent.click(saveButton);
      });

      await waitFor(() => {
        expect(pushToast).toHaveBeenCalledWith({
          message: "Pool updated",
          status: "success",
        });
      });
    });
  });
});
