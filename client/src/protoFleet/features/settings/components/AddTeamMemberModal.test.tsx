import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AddTeamMemberModal from "./AddTeamMemberModal";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import * as utility from "@/shared/utils/utility";

vi.mock("@/protoFleet/api/useUserManagement");
vi.mock("@/shared/features/toaster");
vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof utility>("@/shared/utils/utility");
  return {
    ...actual,
    copyToClipboard: vi.fn().mockResolvedValue(undefined),
  };
});

const mockCreateUser = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnSuccess = vi.fn();

beforeEach(() => {
  vi.mocked(useUserManagement).mockReturnValue({
    createUser: mockCreateUser,
    listUsers: vi.fn(),
    resetUserPassword: vi.fn(),
    deactivateUser: vi.fn(),
  });

  vi.clearAllMocks();
});

describe("AddTeamMemberModal", () => {
  it("renders step 1 with username input", () => {
    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    expect(getByText("Add team member")).toBeInTheDocument();
    expect(getByLabelText("Username")).toBeInTheDocument();
    expect(getByText("Save")).toBeInTheDocument();
  });

  it("autofocuses the username input on mount", () => {
    const { getByLabelText } = render(<AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);

    const usernameInput = getByLabelText("Username");
    expect(usernameInput).toHaveFocus();
  });

  it("save button is always enabled", () => {
    const { getByText } = render(<AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);

    const saveButton = getByText("Save").closest("button");
    expect(saveButton).not.toBeDisabled();
  });

  it("shows validation error when saving empty username", async () => {
    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "   " } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Username is required")).toBeInTheDocument();
    });
  });

  it("calls createUser with trimmed username on save", async () => {
    mockCreateUser.mockImplementation(({ onSuccess }) => {
      onSuccess("user-123", "testuser", "TempPass123");
    });

    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "  testuser  " } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(mockCreateUser).toHaveBeenCalledWith(
        expect.objectContaining({
          username: "testuser",
        }),
      );
    });
  });

  it("shows error message from API", async () => {
    mockCreateUser.mockImplementation(({ onError }) => {
      onError("Username already exists");
    });

    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "duplicate" } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Username already exists")).toBeInTheDocument();
    });
  });

  it("clears error message when typing", async () => {
    mockCreateUser.mockImplementation(({ onError }) => {
      onError("Username already exists");
    });

    const { getByLabelText, getByText, queryByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "duplicate" } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Username already exists")).toBeInTheDocument();
    });

    fireEvent.change(usernameInput, { target: { value: "newuser" } });

    expect(queryByText("Username already exists")).not.toBeInTheDocument();
  });

  it("transitions to step 2 on successful creation", async () => {
    mockCreateUser.mockImplementation(({ onSuccess }) => {
      onSuccess("user-123", "testuser", "TempPass123!@#");
    });

    const { getByLabelText, getByText, getByTestId } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "testuser" } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Member added")).toBeInTheDocument();
      expect(getByText("TempPass123!@#")).toBeInTheDocument();
      expect(getByTestId("modal")).toBeInTheDocument();
    });
  });

  it("shows loading state during creation", async () => {
    let resolveCreate: any;
    mockCreateUser.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve;
        }),
    );

    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "testuser" } });

    const saveButton = getByText("Save").closest("button");
    fireEvent.click(saveButton!);

    await waitFor(() => {
      const saveButton = getByText("Save").closest("button");
      expect(saveButton).toBeDisabled();
    });

    resolveCreate();
  });

  it("allows copying temporary password", async () => {
    mockCreateUser.mockImplementation(({ onSuccess }) => {
      onSuccess("user-123", "testuser", "TempPass123!@#");
    });

    const { getByLabelText, getByText, getByRole } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "testuser" } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Member added")).toBeInTheDocument();
    });

    const copyButton = getByRole("button", { name: /copy password/i });
    fireEvent.click(copyButton);

    expect(utility.copyToClipboard).toHaveBeenCalledWith("TempPass123!@#");
  });

  it("calls onSuccess and onDismiss when clicking Done", async () => {
    mockCreateUser.mockImplementation(({ onSuccess }) => {
      onSuccess("user-123", "testuser", "TempPass123!@#");
    });

    const { getByLabelText, getByText } = render(
      <AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    const usernameInput = getByLabelText("Username");
    fireEvent.change(usernameInput, { target: { value: "testuser" } });

    const saveButton = getByText("Save");
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(getByText("Member added")).toBeInTheDocument();
    });

    const doneButton = getByText("Done");
    fireEvent.click(doneButton);

    expect(mockOnSuccess).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("calls onDismiss when clicking close button in step 1", async () => {
    const { getByTestId } = render(<AddTeamMemberModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);

    const closeButton = getByTestId("header-icon-button");
    fireEvent.click(closeButton);

    await waitFor(() => {
      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });
});
