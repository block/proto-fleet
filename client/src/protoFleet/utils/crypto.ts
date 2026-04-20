export async function computeSha256(file: File): Promise<string> {
  if (!globalThis.crypto?.subtle) {
    throw new Error(
      "SHA-256 hashing requires a secure context (HTTPS). Enable HTTPS in your deployment configuration.",
    );
  }
  const buffer = await file.arrayBuffer();
  const hashBuffer = await crypto.subtle.digest("SHA-256", new Uint8Array(buffer));
  const hashArray = Array.from(new Uint8Array(hashBuffer));
  return hashArray.map((b) => b.toString(16).padStart(2, "0")).join("");
}
