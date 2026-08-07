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

  window.location.href = sanitizeRedirectPath(from);
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
