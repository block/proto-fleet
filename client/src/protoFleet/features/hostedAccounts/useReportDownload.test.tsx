import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";
import { Blob } from "node:buffer";
import { webcrypto } from "node:crypto";
import { REPORT_DOWNLOAD_BUFFER_BYTES, useReportDownload } from "./useReportDownload";
import { useFleetStore } from "@/protoFleet/store";
import * as utility from "@/shared/utils/utility";

const stableSignal = new AbortController().signal;
const metadata = {
  filename: "report.html",
  mediaType: "text/html",
  sha256: "4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a",
};
beforeEach(() => {
  vi.stubGlobal("isSecureContext", false);
  vi.stubGlobal("showSaveFilePicker", undefined);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it.each(["valid", "http-valid", "http-corrupt", "corrupt", "changed", "missing"])(
  "validates %s streamed download integrity before saving",
  async (mode) => {
    vi.stubGlobal("crypto", mode.startsWith("http-") ? {} : webcrypto);
    vi.stubGlobal("Blob", Blob);
    const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    const stream = async function* () {
      yield {
        ...metadata,
        sha256: mode === "missing" ? "" : metadata.sha256,
        chunk: new Uint8Array([mode.endsWith("corrupt") ? 2 : 1]),
      };
      if (mode === "changed") yield { ...metadata, filename: "different.html", chunk: new Uint8Array([1]) };
    };
    const signal = new AbortController().signal;
    const { result } = renderHook(() => useReportDownload("report", signal, stream, vi.fn()));
    await act(async () => {
      await result.current.download("report.html");
    });
    if (mode.endsWith("valid")) {
      expect(save).toHaveBeenCalledTimes(1);
      expect(result.current.error).toBe("");
    } else {
      expect(save).not.toHaveBeenCalled();
      expect(result.current.error).toMatch(/integrity|metadata/);
    }
  },
);

it("does not save after scope cancellation while the stream completes", async () => {
  vi.stubGlobal("Blob", Blob);
  let finish!: () => void;
  const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
  const controller = new AbortController();
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      controller.signal,
      async function* () {
        yield { ...metadata, chunk: new Uint8Array([1]) };
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
      },
      vi.fn(),
    ),
  );
  let pending!: Promise<void>;
  await act(async () => {
    pending = result.current.download("report.html");
  });
  controller.abort();
  await act(async () => {
    finish();
    await pending;
  });
  expect(save).not.toHaveBeenCalled();
});

function nativeFile() {
  const writable = {
    write: vi.fn().mockResolvedValue(undefined),
    close: vi.fn().mockResolvedValue(undefined),
    abort: vi.fn().mockResolvedValue(undefined),
  };
  const picker = vi.fn().mockResolvedValue({ createWritable: vi.fn().mockResolvedValue(writable) });
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("showSaveFilePicker", picker);
  return { picker, writable };
}

it("opens the native picker before the RPC and commits only a complete verified file", async () => {
  const { picker, writable } = nativeFile();
  const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
  const arrayBuffer = vi.spyOn(Blob.prototype, "arrayBuffer");
  const stream = vi.fn(async function* () {
    yield { ...metadata, chunk: new Uint8Array([1]) };
  });
  const { result } = renderHook(() => useReportDownload("report", stableSignal, stream, vi.fn()));
  await act(async () => {
    await result.current.download("report.html");
  });
  expect(picker).toHaveBeenCalledWith({ suggestedName: "report.html" });
  expect(picker.mock.invocationCallOrder[0]).toBeLessThan(stream.mock.invocationCallOrder[0]);
  expect(writable.write).toHaveBeenCalledWith(new Uint8Array([1]));
  expect(writable.close).toHaveBeenCalledTimes(1);
  expect(writable.write.mock.invocationCallOrder[0]).toBeLessThan(writable.close.mock.invocationCallOrder[0]);
  expect(writable.abort).not.toHaveBeenCalled();
  expect(save).not.toHaveBeenCalled();
  expect(arrayBuffer).not.toHaveBeenCalled();
});

it.each(["corrupt", "changed", "revoked", "failure"])(
  "aborts the native file on %s without committing partial bytes",
  async (mode) => {
    const { writable } = nativeFile();
    const denied = vi.fn();
    const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    let released = false;
    const stream = async function* () {
      try {
        yield { ...metadata, chunk: new Uint8Array([mode === "corrupt" ? 2 : 1]) };
        if (mode === "changed") yield { ...metadata, filename: "different.html", chunk: new Uint8Array([1]) };
        if (mode === "revoked") throw new ConnectError("Access revoked", Code.PermissionDenied);
        if (mode === "failure") throw new Error("Download interrupted");
      } finally {
        released = true;
      }
    };
    const { result } = renderHook(() => useReportDownload("report", stableSignal, stream, denied));
    await act(async () => {
      await result.current.download("report.html");
    });
    expect(writable.abort).toHaveBeenCalledTimes(1);
    expect(writable.close).not.toHaveBeenCalled();
    expect(save).not.toHaveBeenCalled();
    expect(released).toBe(true);
    expect(denied).toHaveBeenCalledTimes(mode === "revoked" ? 1 : 0);
  },
);

it("cancels an in-flight native write and releases the export without closing the file", async () => {
  const { writable } = nativeFile();
  let finish!: () => void;
  writable.write.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const controller = new AbortController();
  let released = false;
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      controller.signal,
      async function* () {
        try {
          yield { ...metadata, chunk: new Uint8Array([1]) };
        } finally {
          released = true;
        }
      },
      vi.fn(),
    ),
  );
  let pending!: Promise<void>;
  await act(async () => {
    pending = result.current.download("report.html");
  });
  controller.abort();
  expect(writable.abort).toHaveBeenCalledTimes(1);
  await act(async () => {
    finish();
    await pending;
  });
  expect(released).toBe(true);
  expect(writable.close).not.toHaveBeenCalled();
});

it("does not start an export or fall back when the native picker is canceled", async () => {
  const { picker, writable } = nativeFile();
  picker.mockRejectedValue(new DOMException("Canceled", "AbortError"));
  const stream = vi.fn(async function* () {
    yield { ...metadata, chunk: new Uint8Array([1]) };
  });
  const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
  const { result } = renderHook(() => useReportDownload("report", stableSignal, stream, vi.fn()));
  await act(async () => {
    await result.current.download("report.html");
  });
  expect(stream).not.toHaveBeenCalled();
  expect(writable.write).not.toHaveBeenCalled();
  expect(save).not.toHaveBeenCalled();
  expect(result.current.error).toBe("");
  expect(result.current.busy).toBe(false);
});

it.each([false, true])(
  "enforces the exact fallback buffer boundary (overflow=%s) and releases the stream",
  async (overflow) => {
    vi.stubGlobal("Blob", Blob);
    const chunk = new Uint8Array(REPORT_DOWNLOAD_BUFFER_BYTES);
    const digest = bytesToHex(sha256(chunk));
    const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    const arrayBuffer = vi.spyOn(Blob.prototype, "arrayBuffer");
    let released = false;
    let requestSignal!: AbortSignal;
    const stream = async function* (_name: string, signal: AbortSignal) {
      requestSignal = signal;
      try {
        yield { ...metadata, sha256: digest, chunk };
        if (overflow) yield { ...metadata, sha256: digest, chunk: new Uint8Array([1]) };
      } finally {
        released = true;
      }
    };
    const { result } = renderHook(() => useReportDownload("report", stableSignal, stream, vi.fn()));
    await act(async () => {
      await result.current.download("report.html");
    });
    expect(released).toBe(true);
    expect(requestSignal.aborted).toBe(true);
    expect(arrayBuffer).not.toHaveBeenCalled();
    if (overflow) {
      expect(save).not.toHaveBeenCalled();
      expect(result.current.error).toMatch(/32 MiB.*Ask your host for the complete file/);
    } else {
      expect(save).toHaveBeenCalledTimes(1);
      expect(save.mock.calls[0][0].size).toBe(REPORT_DOWNLOAD_BUFFER_BYTES);
      expect(result.current.error).toBe("");
    }
  },
);

it("streams complete native files larger than the fallback memory budget", async () => {
  const { writable } = nativeFile();
  const chunk = new Uint8Array(REPORT_DOWNLOAD_BUFFER_BYTES);
  const tail = new Uint8Array([1]);
  const digest = bytesToHex(sha256.create().update(chunk).update(tail).digest());
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      stableSignal,
      async function* () {
        yield { ...metadata, sha256: digest, chunk };
        yield { ...metadata, sha256: digest, chunk: tail };
      },
      vi.fn(),
    ),
  );
  await act(async () => {
    await result.current.download("report.html");
  });
  expect(writable.write).toHaveBeenCalledTimes(2);
  expect(writable.close).toHaveBeenCalledTimes(1);
  expect(writable.abort).not.toHaveBeenCalled();
});

it("awaits each native write before pulling another chunk", async () => {
  const { writable } = nativeFile();
  let finish!: () => void;
  writable.write.mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  let pulledNext = false;
  const digest = bytesToHex(sha256(new Uint8Array([1, 2])));
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      stableSignal,
      async function* () {
        yield { ...metadata, sha256: digest, chunk: new Uint8Array([1]) };
        pulledNext = true;
        yield { ...metadata, sha256: digest, chunk: new Uint8Array([2]) };
      },
      vi.fn(),
    ),
  );
  let pending!: Promise<void>;
  await act(async () => {
    pending = result.current.download("report.html");
  });
  expect(pulledNext).toBe(false);
  expect(writable.close).not.toHaveBeenCalled();
  await act(async () => {
    finish();
    await pending;
  });
  expect(pulledNext).toBe(true);
  expect(writable.close).toHaveBeenCalledTimes(1);
});

it("aborts an uncommitted native file immediately when the session changes", async () => {
  const { writable } = nativeFile();
  const original = useFleetStore.getState().auth;
  let finish!: () => void;
  writable.write.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      stableSignal,
      async function* () {
        yield { ...metadata, chunk: new Uint8Array([1]) };
      },
      vi.fn(),
    ),
  );
  let pending!: Promise<void>;
  try {
    await act(async () => {
      pending = result.current.download("report.html");
    });
    useFleetStore.setState({ auth: { ...original, sessionGeneration: original.sessionGeneration + 1 } });
    expect(writable.abort).toHaveBeenCalledTimes(1);
    await act(async () => {
      finish();
      await pending;
    });
    expect(writable.close).not.toHaveBeenCalled();
  } finally {
    useFleetStore.setState({ auth: original });
  }
});

it("uses the fallback on insecure origins even if a picker is exposed", async () => {
  const { picker, writable } = nativeFile();
  vi.stubGlobal("isSecureContext", false);
  vi.stubGlobal("Blob", Blob);
  const save = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
  const { result } = renderHook(() =>
    useReportDownload(
      "report",
      stableSignal,
      async function* () {
        yield { ...metadata, chunk: new Uint8Array([1]) };
      },
      vi.fn(),
    ),
  );
  await act(async () => {
    await result.current.download("report.html");
  });
  expect(picker).not.toHaveBeenCalled();
  expect(writable.write).not.toHaveBeenCalled();
  expect(save).toHaveBeenCalledTimes(1);
});
