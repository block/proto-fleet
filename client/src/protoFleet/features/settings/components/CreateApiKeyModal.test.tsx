import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CreateApiKeyModal from "./CreateApiKeyModal";
import { useApiKeys } from "@/protoFleet/api/useApiKeys";
import * as utility from "@/shared/utils/utility";

vi.mock("@/protoFleet/api/useApiKeys");
vi.mock("@/shared/features/toaster");
vi.mock("@/shared/utils/utility", async () => {
  const actual = await vi.importActual<typeof utility>("@/shared/utils/utility");
  return {
    ...actual,
    copyToClipboard: vi.fn().mockResolvedValue(undefined),
  };
});

const mockCreateApiKey = vi.fn();
const mockOnDismiss = vi.fn();
const mockOnSuccess = vi.fn();

beforeEach(() => {
  vi.mocked(useApiKeys).mockReturnValue({
    createApiKey: mockCreateApiKey,
    listApiKeys: vi.fn(),
    revokeApiKey: vi.fn(),
  });

  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("CreateApiKeyModal", () => {
  it("renders the create step", () => {
    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    expect(getByText("Create API key")).toBeInTheDocument();
    expect(getByLabelText("Key name")).toBeInTheDocument();
    expect(getByLabelText("Expiration date (optional)")).toBeInTheDocument();
    expect(getByText("Create")).toBeInTheDocument();
  });

  it("shows a validation error when the name is empty", async () => {
    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "   " } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("Name is required")).toBeInTheDocument();
    });
  });

  it("calls createApiKey with a trimmed name", async () => {
    mockCreateApiKey.mockResolvedValue("pf_test_secret");

    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "  CI integration  " } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(mockCreateApiKey).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "CI integration",
        }),
      );
    });
  });

  it("submits the expiration date selected from the date picker", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-04-08T12:00:00"));
    mockCreateApiKey.mockResolvedValue("pf_test_secret");

    const { getByLabelText, getByTestId, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByTestId("api-key-expires-trigger"));
    fireEvent.click(getByTestId("api-key-expires-calendar-day-10"));
    fireEvent.click(getByText("Create"));

    expect(mockCreateApiKey).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "CI integration",
      }),
    );

    const [{ expiresAt }] = mockCreateApiKey.mock.calls[0];
    expect(expiresAt).toBeInstanceOf(Date);
    expect(expiresAt.getFullYear()).toBe(2026);
    expect(expiresAt.getMonth()).toBe(3);
    expect(expiresAt.getDate()).toBe(10);
    expect(expiresAt.getHours()).toBe(23);
    expect(expiresAt.getMinutes()).toBe(59);
    expect(expiresAt.getSeconds()).toBe(59);
  });

  it("renders the standard success dialog after creation", async () => {
    mockCreateApiKey.mockResolvedValue("pf_test_secret");

    const { getByLabelText, getByText, getByTestId } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
      expect(getByText("Copy this key now and store it securely. It won't be shown again.")).toBeInTheDocument();
      expect(getByTestId("api-key-value")).toHaveTextContent("pf_test_secret");
      expect(getByText("Done")).toBeInTheDocument();
    });
  });

  it("allows copying the generated API key", async () => {
    mockCreateApiKey.mockResolvedValue("pf_test_secret");

    const { getByLabelText, getByText, getByRole } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
    });

    fireEvent.click(getByRole("button", { name: /copy api key/i }));
    expect(utility.copyToClipboard).toHaveBeenCalledWith("pf_test_secret");
  });

  it("calls onSuccess and onDismiss when Done is clicked", async () => {
    mockCreateApiKey.mockResolvedValue("pf_test_secret");

    const { getByLabelText, getByText } = render(
      <CreateApiKeyModal onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(getByText("API key created")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Done"));
    expect(mockOnSuccess).toHaveBeenCalled();
    expect(mockOnDismiss).toHaveBeenCalled();
  });

  it("ignores a stale create success after the modal is dismissed", async () => {
    let resolveCreate: ((value: string) => void) | undefined;

    mockCreateApiKey.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve;
        }),
    );

    const { getByLabelText, getByText, queryByText, queryByTestId, rerender } = render(
      <CreateApiKeyModal open onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />,
    );

    fireEvent.change(getByLabelText("Key name"), { target: { value: "CI integration" } });
    fireEvent.click(getByText("Create"));

    await waitFor(() => {
      expect(mockCreateApiKey).toHaveBeenCalled();
    });

    rerender(<CreateApiKeyModal open={false} onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);
    rerender(<CreateApiKeyModal open onDismiss={mockOnDismiss} onSuccess={mockOnSuccess} />);

    await act(async () => {
      resolveCreate?.("pf_test_secret");
    });

    expect(queryByText("API key created")).not.toBeInTheDocument();
    expect(queryByTestId("api-key-value")).not.toBeInTheDocument();
    expect(getByText("Create API key")).toBeInTheDocument();
  });
});
