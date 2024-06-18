import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { urlValidationErrors } from "../../components/MiningPools/PoolForm/constants";

import Onboarding from "./Onboarding";

vi.mock("react-router-dom", () => ({
  ...vi.importActual("react-router-dom"),
  useNavigate: () => ({
    Navigation: vi.fn(),
  }),
}));

const poolUrl = "stratum+tcp://ckpool:3333";

// data-testid constants
const miningPoolTitle = "mining-pool-title";
const coolingTitle = "cooling-title";
const coolingTab = "cooling-tab";
const poolsTab = "pools-tab";
const continueButton = "continue-button";
const testConnectionButton = "test-connection-button";
const backupPoolAddButton = "backup-pool-1-add-button";
const backupPoolSaveButton = "backup-pool-save-button";
const backupPoolSavedUrl = "backup-pool-1-saved-url";
const backupPoolDismissButton = "header-icon-button";
const backupPoolDeleteButton = "backup-pool-delete-button";
const continueEditingButton = "continue-editing-button";
const discardChangesButton = "discard-changes-button";
const keepBackupButton = "keep-backup-button";
const deleteBackupButton = "delete-backup-button";
const continueWithoutBackupButton = "continue-without-backup-button";
const finishSetupButton = "finish-setup-button";
const continueToDashboardButton = "continue-to-dashboard-button";
const urlInput = "url-0-input";
const backupUrlInput = "url-1-input";
const validationError = "url-0-input-validation-error";
const poolNotConnectedCallout = "pool-not-connected-callout";
const warnDefaultPoolCallout = "warn-default-pool-callout";
const warnBackupPoolDialog = "warn-backup-pool-dialog";
const warnDiscardDialog = "warn-discard-dialog";
const warnDeleteDialog = "warn-delete-dialog";

describe("Onboarding", () => {
  let component: ReturnType<typeof render>;
  let getByTestId: typeof component.getByTestId;
  let queryByTestId: typeof component.queryByTestId;

  beforeEach(() => {
    component = render(<Onboarding />);
    getByTestId = component.getByTestId;
    queryByTestId = component.queryByTestId;
  });

  test("Renders onboarding on pools tab", () => {
    expect(getByTestId(miningPoolTitle)).toBeInTheDocument();
  });

  test("Cooling tab is initially disabled", () => {
    expect(getByTestId(coolingTab)).toHaveClass("hover:cursor-not-allowed");
  });

  test("Renders callout on clicking continue with no default pool URL inputted", () => {
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    fireEvent.click(getByTestId(continueButton));
    // callout no longer has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).not.toHaveClass("max-h-0");
  });

  test("Renders validation message on clicking test connection with no pool URL inputted", () => {
    const { getByText, queryByText } = within(getByTestId(validationError));
    expect(queryByText(urlValidationErrors.required)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(testConnectionButton));
    expect(getByText(urlValidationErrors.required)).toBeInTheDocument();
  });

  test("Renders callout on clicking test connection with pool URL inputted", async () => {
    const { queryByText } = within(getByTestId(validationError));
    // callout has max-height of 0
    expect(getByTestId(poolNotConnectedCallout)).toHaveClass("max-h-0");
    // enter pool URL and click test connection
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(testConnectionButton));
    // validation error should not show
    expect(queryByText(urlValidationErrors.required)).not.toBeInTheDocument();
    // wait until test connection is done and callout no longer has max-height of 0
    await waitFor(() => {
      expect(getByTestId(poolNotConnectedCallout)).not.toHaveClass("max-h-0");
    });
  });

  test("Does not render callout on clicking continue with default pool URL inputted", () => {
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(continueButton));
    // callout still has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
  });

  test("Renders warning dialog on clicking continue with no backup pools inputted", () => {
    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(continueButton));
    expect(getByTestId(warnDefaultPoolCallout)).toBeInTheDocument();
  });

  test("Does not render warning dialog on clicking continue with at least one backup pool inputted", () => {
    // input default pool URL
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    // click add button of backup pool 1
    fireEvent.click(getByTestId(backupPoolAddButton));
    // input backup pool URL
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    // click save button
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(continueButton));
    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
  });

  test("Can edit backup", () => {
    // add backup pool
    expect(queryByTestId(backupPoolSavedUrl)).not.toBeInTheDocument();
    let addButtonWrapper = within(getByTestId(backupPoolAddButton));
    expect(addButtonWrapper.getByText("Add")).toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    let saveButtonWrapper = within(getByTestId(backupPoolSaveButton));
    expect(saveButtonWrapper.getByText("Add")).toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolSaveButton));
    let backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
    expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    expect(backupPoolSavedUrlWrapper.getByText(poolUrl)).toBeInTheDocument();
    // edit backup pool
    addButtonWrapper = within(getByTestId(backupPoolAddButton));
    expect(addButtonWrapper.getByText("Edit")).toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolAddButton));
    const newPoolUrl = "stratum+tcp://ckpool:4444";
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: newPoolUrl },
    });
    saveButtonWrapper = within(getByTestId(backupPoolSaveButton));
    expect(saveButtonWrapper.getByText("Save")).toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolSaveButton));
    backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
    expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    expect(backupPoolSavedUrlWrapper.getByText(newPoolUrl)).toBeInTheDocument();
  });

  test("Renders discard warning for dismissing unsaved backup pool", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    expect(queryByTestId(warnDiscardDialog)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
  });

  test("Does not renders discard warning for dismissing unchanged backup pool", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.click(getByTestId(backupPoolDismissButton));
    expect(queryByTestId(warnDiscardDialog)).not.toBeInTheDocument();
  });

  test("Can continue editing backup pool on clicking continue editing", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
    expect(queryByTestId(backupUrlInput)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(continueEditingButton));
    expect(getByTestId(backupUrlInput)).toBeInTheDocument();
    expect(getByTestId(backupUrlInput)).toHaveValue(poolUrl);
  });

  test("Can discard changes to backup pool", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    const newPoolUrl = "stratum+tcp://ckpool:4444";
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: newPoolUrl },
    });
    fireEvent.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
    fireEvent.click(getByTestId(discardChangesButton));
    let backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
    expect(backupPoolSavedUrlWrapper.getByText(poolUrl)).toBeInTheDocument();
  });

  test("Renders delete warning for deleting backup pool", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    expect(queryByTestId(warnDeleteDialog)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
  });

  test("Goes back to editing backup pool on clicking keep backup", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
    expect(queryByTestId(backupUrlInput)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(keepBackupButton));
    expect(getByTestId(backupUrlInput)).toBeInTheDocument();
    expect(getByTestId(backupUrlInput)).toHaveValue(poolUrl);
  });

  test("Deletes backup pool on clicking delete backup", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.change(getByTestId(backupUrlInput), {
      target: { value: poolUrl },
    });
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    expect(queryByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    fireEvent.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
    fireEvent.click(getByTestId(deleteBackupButton));
    await waitFor(() => {
      expect(queryByTestId(backupPoolSavedUrl)).not.toBeInTheDocument();
    });
  });

  test("Continues to cooling tab on inputting pool info and clicking continue", () => {
    // input default pool URL
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(continueButton));
    fireEvent.click(getByTestId(continueWithoutBackupButton));
    expect(getByTestId(coolingTitle)).toBeInTheDocument();
    expect(getByTestId(coolingTab)).not.toHaveClass("hover:cursor-not-allowed");
  });

  test("Can switch tabs after pool info is inputted", () => {
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(continueButton));
    fireEvent.click(getByTestId(continueWithoutBackupButton));
    expect(queryByTestId(miningPoolTitle)).not.toBeInTheDocument();
    expect(getByTestId(coolingTitle)).toBeInTheDocument();
    fireEvent.click(getByTestId(poolsTab));
    expect(queryByTestId(coolingTitle)).not.toBeInTheDocument();
    expect(getByTestId(miningPoolTitle)).toBeInTheDocument();
  });

  test("Click on finish setup shows setting up screen", async () => {
    fireEvent.change(getByTestId(urlInput), { target: { value: poolUrl } });
    fireEvent.click(getByTestId(continueButton));
    fireEvent.click(getByTestId(continueWithoutBackupButton));
    fireEvent.click(getByTestId(finishSetupButton));
    expect(getByTestId(continueToDashboardButton)).not.toHaveClass(
      "opacity-100"
    );
    // wait until setup is done and continue to dashboard button shows up
    await waitFor(
      () => {
        expect(getByTestId(continueToDashboardButton)).toHaveClass(
          "opacity-100"
        );
      },
      { timeout: 3000 }
    );
  });
});
