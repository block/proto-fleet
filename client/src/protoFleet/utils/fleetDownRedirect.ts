/**
 * Redirects to the original page stored in the "from" query parameter,
 * or to the home page if no parameter exists.
 *
 * Security: only same-origin paths are allowed, to prevent open redirect
 * vulnerabilities.
 */
export const redirectFromFleetDown = () => {
  const params = new URLSearchParams(window.location.search);
  const from = params.get("from") || "/";

  // Prefixing the current origin makes it structurally impossible for the
  // query parameter to control the scheme or host, regardless of what the
  // sanitizer below returns. This shape is also verifiable by static
  // analysis: CodeQL's redirect query only flags values that can control
  // the start of the URL.
  window.location.href = window.location.origin + sanitizeRedirectPath(from);
};

/**
 * Returns `from` unchanged when it resolves to a same-origin path, "/" otherwise.
 *
 * Validation goes through the URL parser rather than string prefix checks:
 * browsers normalize "\" to "/" when parsing URLs, so a payload like
 * "/\evil.com" passes a startsWith("//") rejection yet still navigates to
 * the external protocol-relative URL "//evil.com".
 */
const sanitizeRedirectPath = (from: string): string => {
  if (!from.startsWith("/") || from.includes("\\")) {
    return "/";
  }

  try {
    const resolved = new URL(from, window.location.origin);
    if (resolved.origin !== window.location.origin) {
      return "/";
    }
  } catch {
    return "/";
  }

  return from;
};
