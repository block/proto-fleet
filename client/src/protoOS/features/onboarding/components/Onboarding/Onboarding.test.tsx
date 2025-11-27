import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Onboarding from "./Onboarding";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { urlValidationErrors } from "@/shared/components/MiningPools/PoolForm/constants";

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

// data-testid constants
const miningPoolTitle = "mining-pool-title";
const testConnectionButton = "test-connection-button";
const backupPoolAddButton = "pool-1-add-button";
const backupPoolSaveButton = "pool-save-button";
const backupPoolSavedUrl = "pool-1-saved-url";
const backupPoolDismissButton = "header-icon-button";
const backupPoolDeleteButton = "pool-delete-button";
const continueEditingButton = "continue-editing-button";
const discardChangesButton = "discard-changes-button";
const keepBackupButton = "keep-backup-button";
const deleteBackupButton = "delete-backup-button";
const finishSetupButton = "finish-setup-button";
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
    component = render(
      <MinerHostingProvider>
        <Onboarding />
      </MinerHostingProvider>,
    );
    getByTestId = component.getByTestId;
    queryByTestId = component.queryByTestId;
  });

  test("Renders onboarding on pools tab", () => {
    expect(getByTestId(miningPoolTitle)).toBeInTheDocument();
  });

  test("Renders callout on clicking continue with no default pool URL inputted", () => {
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    fireEvent.click(getByTestId(finishSetupButton));
    // callout no longer has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).not.toHaveClass("max-h-0");
  });

  test("Renders validation message on clicking test connection with no pool URL inputted", async () => {
    const { getByText, queryByText } = within(getByTestId(validationError));
    expect(queryByText(urlValidationErrors.required)).not.toBeInTheDocument();
    fireEvent.click(getByTestId(testConnectionButton));
    await waitFor(() => {
      expect(getByText(urlValidationErrors.required)).toBeInTheDocument();
    });
  });

  test("Renders callout on clicking test connection with pool URL inputted", async () => {
    const user = userEvent.setup();
    const { queryByText } = within(getByTestId(validationError));
    // callout has max-height of 0
    expect(getByTestId(poolNotConnectedCallout)).toHaveClass("max-h-0");
    // enter pool URL and click test connection
    const input = getByTestId(urlInput);
    await user.clear(input);
    await user.type(input, poolUrl);

    await user.click(getByTestId(testConnectionButton));

    // Wait for validation to clear
    await waitFor(() => {
      expect(queryByText(urlValidationErrors.required)).not.toBeInTheDocument();
    });

    // wait until test connection is done and callout no longer has max-height of 0
    await waitFor(() => {
      expect(getByTestId(poolNotConnectedCallout)).not.toHaveClass("max-h-0");
    });
  });

  test("Does not render callout on clicking continue with default pool URL inputted", async () => {
    const user = userEvent.setup();
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    const input = getByTestId(urlInput);
    await user.clear(input);
    await user.type(input, poolUrl);

    await user.click(getByTestId(finishSetupButton));

    // Wait a bit for any state changes
    await waitFor(() => {
      // callout still has max-height of 0
      expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    });
  });

  test("Renders warning dialog on clicking continue with no backup pools inputted", () => {
    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
    const urlInputElement = getByTestId(urlInput);
    fireEvent.change(urlInputElement, { target: { value: poolUrl } });
    fireEvent.blur(urlInputElement);
    fireEvent.click(getByTestId(finishSetupButton));
    expect(getByTestId(warnDefaultPoolCallout)).toBeInTheDocument();
  });

  test("Does not render warning dialog on clicking continue with at least one backup pool inputted", () => {
    // input default pool URL
    const urlInputElement = getByTestId(urlInput);
    fireEvent.change(urlInputElement, { target: { value: poolUrl } });
    fireEvent.blur(urlInputElement);
    // click add button of backup pool 1
    fireEvent.click(getByTestId(backupPoolAddButton));
    // input backup pool URL
    const backupUrlInputElement = getByTestId(backupUrlInput);
    fireEvent.change(backupUrlInputElement, {
      target: { value: poolUrl },
    });
    fireEvent.blur(backupUrlInputElement);
    // click save button
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(finishSetupButton));
    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
  });

  test("Can edit backup", async () => {
    const user = userEvent.setup();
    // add backup pool
    expect(queryByTestId(backupPoolSavedUrl)).not.toBeInTheDocument();
    let addButtonWrapper = within(getByTestId(backupPoolAddButton));
    expect(addButtonWrapper.getByText("Add")).toBeInTheDocument();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    let saveButtonWrapper = within(getByTestId(backupPoolSaveButton));
    expect(saveButtonWrapper.getByText("Add")).toBeInTheDocument();
    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for saved URL to appear
    await waitFor(() => {
      expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    });

    let backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
    expect(backupPoolSavedUrlWrapper.getByText(poolUrl)).toBeInTheDocument();

    // edit backup pool
    addButtonWrapper = within(getByTestId(backupPoolAddButton));
    expect(addButtonWrapper.getByText("Edit")).toBeInTheDocument();
    await user.click(getByTestId(backupPoolAddButton));
    const newPoolUrl = "stratum+tcp://ckpool:4444";

    await user.clear(backupInput);
    await user.type(backupInput, newPoolUrl);

    saveButtonWrapper = within(getByTestId(backupPoolSaveButton));
    expect(saveButtonWrapper.getByText("Save")).toBeInTheDocument();
    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for updated URL to appear
    await waitFor(() => {
      backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
      expect(backupPoolSavedUrlWrapper.getByText(newPoolUrl)).toBeInTheDocument();
    });
  });

  test("Renders discard warning for dismissing unsaved backup pool", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    expect(queryByTestId(warnDiscardDialog)).not.toBeInTheDocument();
    await user.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
  });

  test("Does not renders discard warning for dismissing unchanged backup pool", async () => {
    fireEvent.click(getByTestId(backupPoolAddButton));
    const backupUrlInputElement = getByTestId(backupUrlInput);
    fireEvent.change(backupUrlInputElement, {
      target: { value: poolUrl },
    });
    fireEvent.blur(backupUrlInputElement);
    fireEvent.click(getByTestId(backupPoolSaveButton));
    fireEvent.click(getByTestId(backupPoolAddButton));
    fireEvent.click(getByTestId(backupPoolDismissButton));
    expect(queryByTestId(warnDiscardDialog)).not.toBeInTheDocument();
  });

  test("Can continue editing backup pool on clicking continue editing", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    await user.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
    expect(queryByTestId(backupUrlInput)).not.toBeInTheDocument();
    await user.click(getByTestId(continueEditingButton));

    // Wait for input to reappear
    await waitFor(() => {
      expect(getByTestId(backupUrlInput)).toBeInTheDocument();
    });

    expect(getByTestId(backupUrlInput)).toHaveValue(poolUrl);
  });

  test("Can discard changes to backup pool", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    // Wait for input to be updated
    await waitFor(() => {
      expect((getByTestId(backupUrlInput) as HTMLInputElement).value).toBe(poolUrl);
    });

    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for saved URL to appear
    await waitFor(() => {
      expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    });

    await user.click(getByTestId(backupPoolAddButton));
    const newPoolUrl = "stratum+tcp://ckpool:4444";

    await user.clear(backupInput);
    await user.type(backupInput, newPoolUrl);

    // Wait for new input to be updated
    await waitFor(() => {
      expect((getByTestId(backupUrlInput) as HTMLInputElement).value).toBe(newPoolUrl);
    });

    await user.click(getByTestId(backupPoolDismissButton));
    await waitFor(() => {
      expect(getByTestId(warnDiscardDialog)).toBeInTheDocument();
    });
    await user.click(getByTestId(discardChangesButton));

    // Wait for dialog to close and check saved URL
    await waitFor(() => {
      let backupPoolSavedUrlWrapper = within(getByTestId(backupPoolSavedUrl));
      expect(backupPoolSavedUrlWrapper.getByText(poolUrl)).toBeInTheDocument();
    });
  });

  test("Renders delete warning for deleting backup pool", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    // Wait for input to be updated
    await waitFor(() => {
      expect((getByTestId(backupUrlInput) as HTMLInputElement).value).toBe(poolUrl);
    });

    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for saved URL to appear
    await waitFor(() => {
      expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    });

    await user.click(getByTestId(backupPoolAddButton));
    expect(queryByTestId(warnDeleteDialog)).not.toBeInTheDocument();
    await user.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
  });

  test("Goes back to editing backup pool on clicking keep backup", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    // Wait for input to be updated
    await waitFor(() => {
      expect((getByTestId(backupUrlInput) as HTMLInputElement).value).toBe(poolUrl);
    });

    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for saved URL to appear
    await waitFor(() => {
      expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    });

    await user.click(getByTestId(backupPoolAddButton));
    await user.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
    expect(queryByTestId(backupUrlInput)).not.toBeInTheDocument();
    await user.click(getByTestId(keepBackupButton));

    // Wait for input to reappear
    await waitFor(() => {
      expect(getByTestId(backupUrlInput)).toBeInTheDocument();
    });

    expect(getByTestId(backupUrlInput)).toHaveValue(poolUrl);
  });

  test("Deletes backup pool on clicking delete backup", async () => {
    const user = userEvent.setup();
    await user.click(getByTestId(backupPoolAddButton));

    const backupInput = getByTestId(backupUrlInput);
    await user.clear(backupInput);
    await user.type(backupInput, poolUrl);

    // Wait for input to be updated
    await waitFor(() => {
      expect((getByTestId(backupUrlInput) as HTMLInputElement).value).toBe(poolUrl);
    });

    await user.click(getByTestId(backupPoolSaveButton));

    // Wait for saved URL to appear
    await waitFor(() => {
      expect(getByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    });

    await user.click(getByTestId(backupPoolAddButton));
    expect(queryByTestId(backupPoolSavedUrl)).toBeInTheDocument();
    await user.click(getByTestId(backupPoolDeleteButton));
    await waitFor(() => {
      expect(getByTestId(warnDeleteDialog)).toBeInTheDocument();
    });
    await user.click(getByTestId(deleteBackupButton));
    await waitFor(() => {
      expect(queryByTestId(backupPoolSavedUrl)).not.toBeInTheDocument();
    });
  });
});
