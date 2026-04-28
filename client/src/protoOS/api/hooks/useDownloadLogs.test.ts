import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useDownloadLogs } from "./useDownloadLogs";
import { useSystemLogs } from "./useSystemLogs";
import { downloadLogs as downloadLogsUtil, formatLogsToCSV } from "@/shared/components/Logs/utility";
import { getFileName } from "@/shared/utils/utility";

vi.mock("./useSystemLogs", () => ({
  useSystemLogs: vi.fn(),
}));

vi.mock("@/shared/components/Logs/utility", () => ({
  downloadLogs: vi.fn(),
  formatLogsToCSV: vi.fn(),
}));

vi.mock("@/shared/utils/utility", () => ({
  getFileName: vi.fn(),
}));

describe("useDownloadLogs", () => {
  const mockFetchData = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();

    (useSystemLogs as Mock).mockReturnValue({
      fetchData: mockFetchData,
    });

    (getFileName as Mock).mockReturnValue("miner-logs-2024-01-29.csv");
  });

  test("calls fetchData with lines parameter", async () => {
    mockFetchData.mockResolvedValue({ content: ["log content"] });
    (formatLogsToCSV as Mock).mockReturnValue(["Time,Type,Message"]);

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    expect(mockFetchData).toHaveBeenCalledWith({ lines: 10000 });
  });

  test("formats logs and downloads CSV", async () => {
    const mockRawLogs = ["raw log content 1", "raw log content 2"];
    const mockCsvData = [
      "Time,Type,Message",
      '2024-01-29 12:00:00,Info,"Test message 1"',
      '2024-01-29 12:01:00,Error,"Test message 2"',
    ];

    mockFetchData.mockResolvedValue({ content: mockRawLogs });
    (formatLogsToCSV as Mock).mockReturnValue(mockCsvData);

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    await waitFor(() => {
      expect(formatLogsToCSV).toHaveBeenCalledWith(mockRawLogs);
      expect(downloadLogsUtil).toHaveBeenCalledWith(mockCsvData, "miner-logs-2024-01-29.csv");
    });
  });

  test("escapes double quotes in log messages", async () => {
    const mockRawLogs = ['raw log with "quotes"'];
    const mockCsvData = ["Time,Type,Message", '2024-01-29 12:00:00,Info,"Test ""quoted"" message"'];

    mockFetchData.mockResolvedValue({ content: mockRawLogs });
    (formatLogsToCSV as Mock).mockReturnValue(mockCsvData);

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    await waitFor(() => {
      expect(formatLogsToCSV).toHaveBeenCalledWith(mockRawLogs);
      expect(downloadLogsUtil).toHaveBeenCalledWith(mockCsvData, "miner-logs-2024-01-29.csv");
    });
  });

  test("does not download if logsResponse is null", async () => {
    mockFetchData.mockResolvedValue(null);

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    await waitFor(() => {
      expect(downloadLogsUtil).not.toHaveBeenCalled();
    });
  });

  test("does not download if content is empty", async () => {
    mockFetchData.mockResolvedValue({ content: null });

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    await waitFor(() => {
      expect(downloadLogsUtil).not.toHaveBeenCalled();
    });
  });

  test("throws error if fetchData fails", async () => {
    const error = new Error("Fetch failed");
    mockFetchData.mockRejectedValue(error);

    const { result } = renderHook(() => useDownloadLogs());

    await expect(result.current.downloadLogs()).rejects.toThrow("Fetch failed");
  });

  test("calls getFileName with correct parameters", async () => {
    mockFetchData.mockResolvedValue({ content: ["log content"] });
    (formatLogsToCSV as Mock).mockReturnValue(["Time,Type,Message", "2024-01-29,Info,Test"]);

    const { result } = renderHook(() => useDownloadLogs());

    await result.current.downloadLogs();

    await waitFor(() => {
      expect(getFileName).toHaveBeenCalledWith("miner-logs", "csv");
    });
  });
});
