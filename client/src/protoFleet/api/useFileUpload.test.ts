import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useFileUpload } from "./useFileUpload";

const mockLogout = vi.fn();

vi.mock("@/protoFleet/store", () => ({
  useLogout: () => mockLogout,
}));

describe("useFileUpload", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("direct upload (XHR)", () => {
    let xhrInstances: MockXHR[];

    class MockXHR {
      open = vi.fn();
      send = vi.fn();
      abort = vi.fn();
      withCredentials = false;
      status = 0;
      statusText = "";
      responseText = "";
      upload = { addEventListener: vi.fn() };
      private listeners: Record<string, (() => void)[]> = {};

      constructor() {
        xhrInstances.push(this);
      }

      addEventListener(event: string, handler: () => void) {
        if (!this.listeners[event]) this.listeners[event] = [];
        this.listeners[event].push(handler);
      }

      trigger(event: string) {
        this.listeners[event]?.forEach((h) => h());
      }
    }

    beforeEach(() => {
      xhrInstances = [];
      vi.stubGlobal("XMLHttpRequest", MockXHR);
    });

    it("sends multipart POST with credentials and resolves parsed JSON", async () => {
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file);

      const xhr = xhrInstances[0];
      expect(xhr.open).toHaveBeenCalledWith("POST", "/upload");
      expect(xhr.withCredentials).toBe(true);
      expect(xhr.send).toHaveBeenCalledWith(expect.any(FormData));

      xhr.status = 200;
      xhr.responseText = JSON.stringify({ id: "abc" });
      xhr.trigger("load");

      await expect(promise).resolves.toEqual({ id: "abc" });
    });

    it("calls logout on 401", async () => {
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file);

      const xhr = xhrInstances[0];
      xhr.status = 401;
      xhr.trigger("load");

      await expect(promise).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("surfaces server error message from JSON body", async () => {
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file);

      const xhr = xhrInstances[0];
      xhr.status = 400;
      xhr.responseText = JSON.stringify({ error: "bad input" });
      xhr.trigger("load");

      await expect(promise).rejects.toThrow("bad input");
      expect(mockLogout).not.toHaveBeenCalled();
    });

    it("reports progress via onProgress", async () => {
      const file = new File(["data"], "file.swu");
      const onProgress = vi.fn();
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file, { onProgress });

      const xhr = xhrInstances[0];
      expect(xhr.upload.addEventListener).toHaveBeenCalledWith("progress", expect.any(Function));

      const handler = xhr.upload.addEventListener.mock.calls[0][1];
      handler({ lengthComputable: true, loaded: 25, total: 100 });
      expect(onProgress).toHaveBeenCalledWith(25);

      xhr.status = 200;
      xhr.responseText = "{}";
      xhr.trigger("load");
      await promise;
    });

    it("aborts on signal", async () => {
      const controller = new AbortController();
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file, { signal: controller.signal });

      controller.abort();
      xhrInstances[0].trigger("abort");

      await expect(promise).rejects.toThrow("Upload was cancelled.");
    });

    it("rejects on network error", async () => {
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const promise = result.current.upload("/upload", file);

      xhrInstances[0].trigger("error");

      await expect(promise).rejects.toThrow("Network error during upload.");
    });

    it("uses custom fieldName", async () => {
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      result.current.upload("/upload", file, { fieldName: "firmware" });

      const xhr = xhrInstances[0];
      const formData: FormData = xhr.send.mock.calls[0][0];
      expect(formData.get("firmware")).toBeTruthy();
    });
  });

  describe("chunked upload (fetch)", () => {
    function mockFetchSequence(...responses: Array<{ status: number; body?: object }>) {
      const mocked = vi.fn();
      for (const { status, body } of responses) {
        mocked.mockResolvedValueOnce({
          ok: status >= 200 && status < 300,
          status,
          statusText: status === 200 ? "OK" : "Error",
          json: () => Promise.resolve(body ?? {}),
        });
      }
      vi.stubGlobal("fetch", mocked);
      return mocked;
    }

    const chunkedConfig = {
      enabled: true,
      chunkSize: 5,
      initiateUrl: "/upload/chunked",
      chunkUrl: (id: string) => `/upload/chunked/${id}`,
      completeUrl: (id: string) => `/upload/chunked/${id}/complete`,
    };

    it("uploads via initiate → PUT chunks → complete", async () => {
      const mockFetch = mockFetchSequence(
        { status: 200, body: { upload_id: "u1" } },
        { status: 200 },
        { status: 200 },
        { status: 200, body: { firmware_file_id: "fw-1" } },
      );

      const file = new File(["a".repeat(10)], "file.swu");
      const { result } = renderHook(() => useFileUpload());
      const data = await result.current.upload("/ignored", file, { chunked: chunkedConfig });

      expect(data).toEqual({ firmware_file_id: "fw-1" });
      expect(mockFetch).toHaveBeenCalledTimes(4);

      expect(mockFetch.mock.calls[0][0]).toBe("/upload/chunked");
      expect(mockFetch.mock.calls[1][0]).toBe("/upload/chunked/u1");
      expect(mockFetch.mock.calls[2][0]).toBe("/upload/chunked/u1");
      expect(mockFetch.mock.calls[3][0]).toBe("/upload/chunked/u1/complete");
    });

    it("calls logout on 401 during initiate", async () => {
      mockFetchSequence({ status: 401 });
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());

      await expect(result.current.upload("/x", file, { chunked: chunkedConfig })).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("calls logout on 401 during chunk upload", async () => {
      mockFetchSequence({ status: 200, body: { upload_id: "u1" } }, { status: 401 });
      const file = new File(["a".repeat(10)], "file.swu");
      const { result } = renderHook(() => useFileUpload());

      await expect(result.current.upload("/x", file, { chunked: chunkedConfig })).rejects.toThrow("Session expired");
      expect(mockLogout).toHaveBeenCalledOnce();
    });

    it("reports progress after each chunk", async () => {
      mockFetchSequence(
        { status: 200, body: { upload_id: "u1" } },
        { status: 200 },
        { status: 200 },
        { status: 200 },
        { status: 200, body: { result: "ok" } },
      );

      const file = new File(["a".repeat(15)], "file.swu");
      const onProgress = vi.fn();
      const { result } = renderHook(() => useFileUpload());
      await result.current.upload("/x", file, { chunked: chunkedConfig, onProgress });

      expect(onProgress).toHaveBeenCalledTimes(3);
      expect(onProgress).toHaveBeenNthCalledWith(1, 33);
      expect(onProgress).toHaveBeenNthCalledWith(2, 67);
      expect(onProgress).toHaveBeenNthCalledWith(3, 100);
    });

    it("respects abort signal between chunks", async () => {
      const controller = new AbortController();
      mockFetchSequence({ status: 200, body: { upload_id: "u1" } }, { status: 200 });

      const file = new File(["a".repeat(15)], "file.swu");
      const { result } = renderHook(() => useFileUpload());

      controller.abort();
      await expect(
        result.current.upload("/x", file, { chunked: chunkedConfig, signal: controller.signal }),
      ).rejects.toThrow();
    });

    it("throws when initiate is missing upload_id", async () => {
      mockFetchSequence({ status: 200, body: {} });
      const file = new File(["data"], "file.swu");
      const { result } = renderHook(() => useFileUpload());

      await expect(result.current.upload("/x", file, { chunked: chunkedConfig })).rejects.toThrow(
        "Server response missing upload_id.",
      );
    });
  });
});
