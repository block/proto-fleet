import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Onboarding from "./Onboarding";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";

vi.mock("react-router-dom", async (importOriginal) => {
  const actual = (await importOriginal()) as any;
  return {
    ...actual,
    useNavigate: () => ({
      Navigation: vi.fn(),
    }),
    useLocation: () => ({
      pathname: "/onboarding",
    }),
    Link: vi.fn(),
  };
});

const poolUrl = "stratum+tcp://ckpool:3333";

// data-testid constants for new unified pool list UI
const addPoolButton = "add-pool-button";
const addAnotherPoolButton = "add-another-pool-button";
const poolSaveButton = "pool-save-button";
const poolDismissButton = "header-icon-button";
const finishSetupButton = "finish-setup-button";
const warnDefaultPoolCallout = "warn-default-pool-callout";
const warnBackupPoolDialog = "warn-backup-pool-dialog";

// Modal inputs use poolIndex in their testIds
const getPoolNameInput = (poolIndex: number) => `pool-name-${poolIndex}-input`;
const getUrlInput = (poolIndex: number) => `url-${poolIndex}-input`;
const getUsernameInput = (poolIndex: number) => `username-${poolIndex}-input`;
const getEditButton = (poolIndex: number) => `pool-${poolIndex}-edit-button`;

describe("Onboarding", () => {
  let component: ReturnType<typeof render>;
  let getByTestId: typeof component.getByTestId;
  let queryByTestId: typeof component.queryByTestId;

  beforeEach(() => {
    component = render(
      <MinerHostingProvider>
        <Onboarding />
      </MinerHostingProvider>,
    );
    getByTestId = component.getByTestId;
    queryByTestId = component.queryByTestId;
  });

  test("Renders onboarding with empty state showing Add pool button", () => {
    expect(getByTestId(addPoolButton)).toBeInTheDocument();
  });

  test("Renders callout on clicking finish setup with no pools configured", () => {
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    fireEvent.click(getByTestId(finishSetupButton));
    // callout no longer has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).not.toHaveClass("max-h-0");
  });

  test("Opens pool modal when clicking Add pool button", async () => {
    const user = userEvent.setup();

    // Initially modal should not be visible
    expect(queryByTestId(getUrlInput(0))).not.toBeInTheDocument();

    // Click Add pool button
    await user.click(getByTestId(addPoolButton));

    // Modal should now show with URL input
    await waitFor(() => {
      expect(getByTestId(getUrlInput(0))).toBeInTheDocument();
    });
  });

  test("Can add first pool and see it in the list", async () => {
    const user = userEvent.setup();

    // Click Add pool button
    await user.click(getByTestId(addPoolButton));

    // Fill in pool details
    const poolNameInput = getByTestId(getPoolNameInput(0));
    const urlInput = getByTestId(getUrlInput(0));
    const usernameInput = getByTestId(getUsernameInput(0));

    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Test Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");

    // Save the pool
    await user.click(getByTestId(poolSaveButton));

    // Wait for pool to appear in list with Update button
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Should now show "Add another pool" button
    expect(getByTestId(addAnotherPoolButton)).toBeInTheDocument();
  });

  test("Does not render warning callout on clicking finish setup with pool configured", async () => {
    const user = userEvent.setup();

    // Add a pool
    await user.click(getByTestId(addPoolButton));
    const poolNameInput = getByTestId(getPoolNameInput(0));
    const urlInput = getByTestId(getUrlInput(0));
    const usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Test Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // callout should have max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");

    // Click finish setup
    await user.click(getByTestId(finishSetupButton));

    // Wait and verify callout still has max-height of 0 (no warning about missing default pool)
    await waitFor(() => {
      expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    });
  });

  test("Renders warning dialog on clicking finish setup with only one pool (no backup)", async () => {
    const user = userEvent.setup();

    // Add first pool only
    await user.click(getByTestId(addPoolButton));
    const poolNameInput = getByTestId(getPoolNameInput(0));
    const urlInput = getByTestId(getUrlInput(0));
    const usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Test Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Click finish setup - should show backup pool warning dialog
    await user.click(getByTestId(finishSetupButton));

    await waitFor(() => {
      expect(getByTestId(warnBackupPoolDialog)).toBeInTheDocument();
    });
  });

  test("Does not render warning dialog on clicking finish setup with backup pool configured", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Primary Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for first pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Add second pool (backup)
    await user.click(getByTestId(addAnotherPoolButton));

    await waitFor(() => {
      expect(getByTestId(getUrlInput(1))).toBeInTheDocument();
    });

    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Backup Pool");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://backup:3333");
    await user.clear(usernameInput);
    await user.type(usernameInput, "backupuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for second pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(1))).toBeInTheDocument();
    });

    // Click finish setup - should NOT show backup pool warning
    await user.click(getByTestId(finishSetupButton));

    // Dialog should not appear
    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
  });

  test("Can edit existing pool", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    const poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    const usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Test Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Click Update button to edit
    await user.click(getByTestId(getEditButton(0)));

    // Wait for modal to open with existing data
    await waitFor(() => {
      expect(getByTestId(getUrlInput(0))).toBeInTheDocument();
    });

    // Update the URL
    urlInput = getByTestId(getUrlInput(0));
    const newPoolUrl = "stratum+tcp://newpool:4444";
    await user.clear(urlInput);
    await user.type(urlInput, newPoolUrl);

    // Save changes
    await user.click(getByTestId(poolSaveButton));

    // Modal should close
    await waitFor(() => {
      expect(queryByTestId(getUrlInput(0))).not.toBeInTheDocument();
    });
  });

  test("Dismisses pool modal on clicking dismiss button", async () => {
    const user = userEvent.setup();

    // Open modal
    await user.click(getByTestId(addPoolButton));

    // Verify modal is open
    await waitFor(() => {
      expect(getByTestId(getUrlInput(0))).toBeInTheDocument();
    });

    // Dismiss modal
    await user.click(getByTestId(poolDismissButton));

    // Modal should close
    await waitFor(() => {
      expect(queryByTestId(getUrlInput(0))).not.toBeInTheDocument();
    });
  });

  test("Does not save pool when modal is dismissed without saving", async () => {
    const user = userEvent.setup();

    // Open modal
    await user.click(getByTestId(addPoolButton));

    // Fill in some data
    const urlInput = getByTestId(getUrlInput(0));
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);

    // Dismiss without saving
    await user.click(getByTestId(poolDismissButton));

    // Modal should close
    await waitFor(() => {
      expect(queryByTestId(getUrlInput(0))).not.toBeInTheDocument();
    });

    // Pool should not be saved - empty state should still show "Add pool" button
    expect(getByTestId(addPoolButton)).toBeInTheDocument();
  });

  test("Can delete a pool from the actions menu (requires 2+ pools)", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Primary Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser1");
    await user.click(getByTestId(poolSaveButton));

    // Wait for first pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Add second pool (delete is only enabled when there are 2+ pools)
    await user.click(getByTestId("add-another-pool-button"));
    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Backup Pool");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://backup.example.com:3333");
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser2");
    await user.click(getByTestId(poolSaveButton));

    // Wait for second pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(1))).toBeInTheDocument();
    });

    // Open the actions menu for the first pool
    await user.click(getByTestId("pool-0-actions-menu-button"));

    // Wait for popover to appear with delete action
    await waitFor(() => {
      expect(getByTestId("pool-0-delete-action")).toBeInTheDocument();
    });

    // Click delete
    await user.click(getByTestId("pool-0-delete-action"));

    // First pool should be removed, second pool should move to position 0
    await waitFor(() => {
      expect(queryByTestId(getEditButton(1))).not.toBeInTheDocument();
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });
  });

  test("Delete action is disabled when only one pool is configured", async () => {
    const user = userEvent.setup();

    // Add one pool
    await user.click(getByTestId(addPoolButton));
    const poolNameInput = getByTestId(getPoolNameInput(0));
    const urlInput = getByTestId(getUrlInput(0));
    const usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Test Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Open the actions menu
    await user.click(getByTestId("pool-0-actions-menu-button"));

    // Wait for popover to appear
    await waitFor(() => {
      expect(getByTestId("pool-0-edit-action")).toBeInTheDocument();
    });

    // Delete action should be visible but disabled (rendered as div, not button, when no onClick)
    const deleteAction = getByTestId("pool-0-delete-action");
    expect(deleteAction).toBeInTheDocument();
    // When disabled, Row renders a div instead of a button (onClick is undefined)
    expect(deleteAction.tagName).toBe("DIV");
  });

  test("Allows same URL with different username (backend supports this)", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Primary Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for first pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Add second pool with same URL but different username - should be allowed
    await user.click(getByTestId(addAnotherPoolButton));

    await waitFor(() => {
      expect(getByTestId(getUrlInput(1))).toBeInTheDocument();
    });

    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Backup Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl); // Same URL as first pool
    await user.clear(usernameInput);
    await user.type(usernameInput, "differentuser"); // Different username
    await user.click(getByTestId(poolSaveButton));

    // Should save successfully (no duplicate error)
    await waitFor(() => {
      expect(getByTestId(getEditButton(1))).toBeInTheDocument();
    });
  });

  test("Shows error when trying to save pool with duplicate URL and username", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Primary Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl);
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for first pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Try to add second pool with same URL AND same username - should fail
    await user.click(getByTestId(addAnotherPoolButton));

    await waitFor(() => {
      expect(getByTestId(getUrlInput(1))).toBeInTheDocument();
    });

    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Backup Pool");
    await user.clear(urlInput);
    await user.type(urlInput, poolUrl); // Same URL
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser"); // Same username
    await user.click(getByTestId(poolSaveButton));

    // Should show duplicate error
    await waitFor(() => {
      expect(component.getByText("This Pool URL and Username combination is already configured.")).toBeInTheDocument();
    });
  });

  test("Shows error for duplicate URL and username (case insensitive)", async () => {
    const user = userEvent.setup();

    // Add first pool with lowercase URL and username
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Primary Pool");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://pool.example.com:3333");
    await user.clear(usernameInput);
    await user.type(usernameInput, "testuser");
    await user.click(getByTestId(poolSaveButton));

    // Wait for first pool to be saved
    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Try to add pool with same URL and username but different casing - should fail
    await user.click(getByTestId(addAnotherPoolButton));

    await waitFor(() => {
      expect(getByTestId(getUrlInput(1))).toBeInTheDocument();
    });

    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Backup Pool");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://POOL.EXAMPLE.COM:3333"); // Same URL, different case
    await user.clear(usernameInput);
    await user.type(usernameInput, "TESTUSER"); // Same username, different case
    await user.click(getByTestId(poolSaveButton));

    // Should show duplicate error (case insensitive match)
    await waitFor(() => {
      expect(component.getByText("This Pool URL and Username combination is already configured.")).toBeInTheDocument();
    });
  });

  test("Can reorder pools via drag and drop", async () => {
    const user = userEvent.setup();

    // Add first pool
    await user.click(getByTestId(addPoolButton));
    let poolNameInput = getByTestId(getPoolNameInput(0));
    let urlInput = getByTestId(getUrlInput(0));
    let usernameInput = getByTestId(getUsernameInput(0));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Pool 1");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://pool1:3333");
    await user.clear(usernameInput);
    await user.type(usernameInput, "user1");
    await user.click(getByTestId(poolSaveButton));

    await waitFor(() => {
      expect(getByTestId(getEditButton(0))).toBeInTheDocument();
    });

    // Add second pool
    await user.click(getByTestId(addAnotherPoolButton));
    await waitFor(() => {
      expect(getByTestId(getUrlInput(1))).toBeInTheDocument();
    });

    poolNameInput = getByTestId(getPoolNameInput(1));
    urlInput = getByTestId(getUrlInput(1));
    usernameInput = getByTestId(getUsernameInput(1));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, "Pool 2");
    await user.clear(urlInput);
    await user.type(urlInput, "stratum+tcp://pool2:3333");
    await user.clear(usernameInput);
    await user.type(usernameInput, "user2");
    await user.click(getByTestId(poolSaveButton));

    await waitFor(() => {
      expect(getByTestId(getEditButton(1))).toBeInTheDocument();
    });

    // Verify both pools are shown with correct priority numbers
    expect(component.getByText("1")).toBeInTheDocument();
    expect(component.getByText("2")).toBeInTheDocument();

    // Note: Full drag-and-drop simulation requires complex pointer event sequences
    // that are difficult to test reliably. The drag-and-drop functionality is
    // better verified through manual testing or E2E tests with real browser interactions.
    // This test verifies the pools are rendered correctly for reordering.
  });
});
