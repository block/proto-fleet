import { useCallback, useMemo } from "react";
import { frigateUpdateClient } from "@/protoFleet/api/clients";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import type { FrigatePackage } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { useFileUpload } from "@/protoFleet/api/useFileUpload";

const UPLOAD_BASE = `${API_PROXY_BASE}/api/v1/frigate-packages/upload/chunked`;

// The server accepts chunks up to 64 MiB and Fleet's bundled proxies allow
// 64 MiB bodies; 32 MiB matches the chunk size the firmware uploader uses.
export const FRIGATE_PACKAGE_CHUNK_SIZE = 32 * 1024 * 1024;
const REQUEST_TIMEOUT_MS = 30_000;

export const FRIGATE_PACKAGE_EXTENSION = ".deb";

export interface FrigatePackageUploadInput {
  /** The Debian package. */
  deb: File;
  /** The signed release statement: the exact JSON bytes that were signed. */
  statement: File;
  /** The ECDSA P-256 ASN.1 DER signature over the statement bytes. */
  signature: File;
  onProgress?: (percent: number) => void;
  signal?: AbortSignal;
}

export interface FrigatePackageUploadResult {
  package: FrigatePackageRecord;
  reused: boolean;
}

/** The REST completion response, which mirrors FrigatePackage in snake_case. */
export interface FrigatePackageRecord {
  id: string;
  filename: string;
  version: string;
  architecture: string;
  sha256: string;
  size_bytes: number;
  repository: string;
  release_key_id: string;
  min_installed_version: string;
  issued_at: string;
  uploaded_at: string;
}

function toBase64(bytes: Uint8Array): string {
  let binary = "";
  const step = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += step) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + step));
  }
  return btoa(binary);
}

async function readBase64(file: File): Promise<string> {
  return toBase64(new Uint8Array(await file.arrayBuffer()));
}

export function validateFrigatePackageFiles(input: Pick<FrigatePackageUploadInput, "deb" | "statement" | "signature">) {
  if (!input.deb.name.toLowerCase().endsWith(FRIGATE_PACKAGE_EXTENSION)) {
    return `The package must be a ${FRIGATE_PACKAGE_EXTENSION} file.`;
  }
  if (input.deb.size === 0) return "The package file is empty.";
  if (input.statement.size === 0) return "The release statement file is empty.";
  if (input.signature.size === 0) return "The release signature file is empty.";
  return null;
}

/** Packages are shared by every site; the server enforces the permissions. */
export function useFrigatePackages() {
  const { upload } = useFileUpload();

  const listPackages = useCallback(async (signal?: AbortSignal): Promise<FrigatePackage[]> => {
    const response = await frigateUpdateClient.listPackages({}, { signal, timeoutMs: REQUEST_TIMEOUT_MS });
    return response.packages;
  }, []);

  const deletePackage = useCallback(async (packageId: string): Promise<void> => {
    await frigateUpdateClient.deletePackage({ packageId }, { timeoutMs: REQUEST_TIMEOUT_MS });
  }, []);

  const uploadPackage = useCallback(
    async ({
      deb,
      statement,
      signature,
      onProgress,
      signal,
    }: FrigatePackageUploadInput): Promise<FrigatePackageUploadResult> => {
      const invalid = validateFrigatePackageFiles({ deb, statement, signature });
      if (invalid) throw new Error(invalid);
      const [statementB64, signatureB64] = await Promise.all([readBase64(statement), readBase64(signature)]);
      const data = (await upload(UPLOAD_BASE, deb, {
        onProgress,
        signal,
        // The server's initiate body names the size "size"; the shared uploader
        // also sends "file_size", which the server ignores.
        initiateFields: { size: deb.size, release_statement_b64: statementB64, release_signature_b64: signatureB64 },
        chunked: {
          enabled: true,
          chunkSize: FRIGATE_PACKAGE_CHUNK_SIZE,
          initiateUrl: UPLOAD_BASE,
          chunkUrl: (id) => `${UPLOAD_BASE}/${encodeURIComponent(id)}`,
          completeUrl: (id) => `${UPLOAD_BASE}/${encodeURIComponent(id)}/complete`,
        },
      })) as Partial<FrigatePackageUploadResult> | null;
      if (!data?.package?.id) throw new Error("Server response missing the uploaded package.");
      return { package: data.package, reused: data.reused === true };
    },
    [upload],
  );

  return useMemo(() => ({ listPackages, deletePackage, uploadPackage }), [listPackages, deletePackage, uploadPackage]);
}
