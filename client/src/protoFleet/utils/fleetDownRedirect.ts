/**
 * Redirects to the original page stored in the "from" query parameter,
 * or to the home page if no parameter exists.
 *
 * Security: Only allows relative paths starting with "/" to prevent open redirect vulnerabilities.
 */
export const redirectFromFleetDown = () => {
  const params = new URLSearchParams(window.location.search);
  const from = params.get("from") || "/";

  // Security: Validate that the redirect URL is a relative path
  // Prevent external URLs, protocol-relative URLs, and JavaScript URLs
  if (!from.startsWith("/") || from.startsWith("//")) {
    window.location.href = "/";
    return;
  }

  window.location.href = from;
};
