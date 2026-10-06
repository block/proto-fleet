// Discriminated union for the SitePicker's current selection. Site IDs come
// back from the proto as bigint, but bigint isn't JSON-serializable; we store
// the decimal string form and convert at the API boundary. Slug is the URL
// segment for site-scoped routes.
//
// Lives in store/types/ (not on a slice) so consumers can import the type
// without pulling the slice and creating a circular dep with useFleetStore.
export type ActiveSite = { kind: "all" } | { kind: "site"; id: string; slug: string } | { kind: "unassigned" };

export const DEFAULT_ACTIVE_SITE: ActiveSite = { kind: "all" };
const SITE_ID_RE = /^[1-9]\d*$/;
const SITE_SLUG_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

// Runtime guard used by the Zustand persist merge step to reject malformed
// localStorage payloads (older schema, manual tampering, partial writes).
export const isActiveSite = (v: unknown): v is ActiveSite => {
  if (!v || typeof v !== "object") return false;
  const kind = (v as { kind?: unknown }).kind;
  if (kind === "all" || kind === "unassigned") return true;
  if (kind === "site") {
    const id = (v as { id?: unknown }).id;
    const slug = (v as { slug?: unknown }).slug;
    return (
      typeof id === "string" &&
      SITE_ID_RE.test(id) &&
      typeof slug === "string" &&
      SITE_SLUG_RE.test(slug) &&
      !slug.includes("--")
    );
  }
  return false;
};

export const sanitizeActiveSite = (v: unknown): ActiveSite => (isActiveSite(v) ? v : DEFAULT_ACTIVE_SITE);
