import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { FrigatePackageSchema } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import {
  FRIGATE_PACKAGE_CHUNK_SIZE,
  useFrigatePackages,
  validateFrigatePackageFiles,
} from "@/protoFleet/api/useFrigatePackages";

const mocks = vi.hoisted(() => ({
  upload: vi.fn(),
  listPackages: vi.fn(),
  deletePackage: vi.fn(),
}));

vi.mock("@/protoFleet/api/useFileUpload", () => ({ useFileUpload: () => ({ upload: mocks.upload }) }));
vi.mock("@/protoFleet/api/clients", () => ({
  frigateUpdateClient: { listPackages: mocks.listPackages, deletePackage: mocks.deletePackage },
}));

const packageRecord = {
  id: "11111111-1111-4111-8111-111111111111",
  filename: "frigate_1.4.0_arm64.deb",
  version: "1.4.0",
  architecture: "arm64",
  sha256: "ab".repeat(32),
  size_bytes: 3,
  repository: "frigate",
  release_key_id: "sha256:cd",
  min_installed_version: "",
  issued_at: "2026-01-01T00:00:00Z",
  uploaded_at: "2026-01-02T00:00:00Z",
};

const files = () => ({
  deb: new File(["deb"], "frigate_1.4.0_arm64.deb"),
  // 0x30 0x80 0xff are not valid UTF-8, so this fails if the bytes are read as text.
  statement: new File([new Uint8Array([123, 125])], "statement.json"),
  signature: new File([new Uint8Array([0x30, 0x80, 0xff])], "statement.sig"),
});

beforeEach(() => vi.clearAllMocks());

describe("validateFrigatePackageFiles", () => {
  it("accepts a .deb with a statement and signature", () => {
    expect(validateFrigatePackageFiles(files())).toBeNull();
  });

  it("rejects a package that is not a .deb", () => {
    expect(validateFrigatePackageFiles({ ...files(), deb: new File(["x"], "frigate.tar.gz") })).toMatch(/\.deb/);
  });

  it.each(["deb", "statement", "signature"] as const)("rejects an empty %s file", (key) => {
    expect(validateFrigatePackageFiles({ ...files(), [key]: new File([], "empty.deb") })).toMatch(/empty/);
  });
});

describe("useFrigatePackages", () => {
  it("uploads through the chunked endpoints with the statement and signature as base64 bytes", async () => {
    mocks.upload.mockResolvedValue({ package: packageRecord, reused: true });
    const { result } = renderHook(() => useFrigatePackages());
    const input = files();

    const uploaded = await result.current.uploadPackage(input);

    expect(uploaded).toEqual({ package: packageRecord, reused: true });
    const [url, file, options] = mocks.upload.mock.calls[0];
    expect(url).toBe("/api-proxy/api/v1/frigate-packages/upload/chunked");
    expect(file).toBe(input.deb);
    expect(options.initiateFields).toEqual({
      size: 3,
      release_statement_b64: btoa("{}"),
      release_signature_b64: btoa(String.fromCharCode(0x30, 0x80, 0xff)),
    });
    expect(options.chunked.enabled).toBe(true);
    expect(options.chunked.chunkSize).toBe(FRIGATE_PACKAGE_CHUNK_SIZE);
    expect(options.chunked.initiateUrl).toBe("/api-proxy/api/v1/frigate-packages/upload/chunked");
    expect(options.chunked.chunkUrl("a b")).toBe("/api-proxy/api/v1/frigate-packages/upload/chunked/a%20b");
    expect(options.chunked.completeUrl("u1")).toBe("/api-proxy/api/v1/frigate-packages/upload/chunked/u1/complete");
  });

  it("forwards upload progress to the caller", async () => {
    mocks.upload.mockImplementation(async (_url, _file, options) => {
      options.onProgress(40);
      options.onProgress(100);
      return { package: packageRecord };
    });
    const onProgress = vi.fn();
    const { result } = renderHook(() => useFrigatePackages());

    const uploaded = await result.current.uploadPackage({ ...files(), onProgress });

    expect(onProgress.mock.calls).toEqual([[40], [100]]);
    expect(uploaded.reused).toBe(false);
  });

  it("rejects with the server's message when the upload fails", async () => {
    mocks.upload.mockRejectedValue(new Error("release signature is not trusted"));
    const { result } = renderHook(() => useFrigatePackages());

    await expect(result.current.uploadPackage(files())).rejects.toThrow("release signature is not trusted");
  });

  it("does not start an upload for invalid files", async () => {
    const { result } = renderHook(() => useFrigatePackages());

    await expect(result.current.uploadPackage({ ...files(), deb: new File(["x"], "frigate.zip") })).rejects.toThrow(
      /\.deb/,
    );
    expect(mocks.upload).not.toHaveBeenCalled();
  });

  it("rejects a completion response without a package", async () => {
    mocks.upload.mockResolvedValue({});
    const { result } = renderHook(() => useFrigatePackages());

    await expect(result.current.uploadPackage(files())).rejects.toThrow(/missing the uploaded package/);
  });

  it("lists and deletes packages through the service", async () => {
    const item = create(FrigatePackageSchema, { id: packageRecord.id, version: "1.4.0" });
    mocks.listPackages.mockResolvedValue({ packages: [item] });
    mocks.deletePackage.mockResolvedValue({});
    const { result } = renderHook(() => useFrigatePackages());

    await expect(result.current.listPackages()).resolves.toEqual([item]);
    await result.current.deletePackage(packageRecord.id);

    expect(mocks.deletePackage).toHaveBeenCalledWith({ packageId: packageRecord.id }, expect.any(Object));
  });

  it("reports whether a container is paired where the user may update controller software", async () => {
    mocks.listPackages.mockResolvedValue({ packages: [], hasUpdatableContainer: true });
    const { result } = renderHook(() => useFrigatePackages());
    const controller = new AbortController();

    await expect(result.current.hasUpdatableContainer(controller.signal)).resolves.toBe(true);
    expect(mocks.listPackages).toHaveBeenCalledWith({}, { signal: controller.signal, timeoutMs: 30_000 });
  });
});
