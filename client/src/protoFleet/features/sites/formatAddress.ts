// Shared site-address formatter. Site detail and modal preview surfaces format
// the same shape, so drift between them would surface as inconsistent display.
// Centralize the join + ordering rules here.

interface AddressParts {
  address?: string | null;
  locationCity?: string | null;
  locationState?: string | null;
  postalCode?: string | null;
  country?: string | null;
}

interface FormatOptions {
  // includeCountry adds the country code as the last segment. The
  // single-site detail view shows it; the list / preview rows omit it
  // because they're already scoped to a known org's sites.
  includeCountry?: boolean;
  // separator between top-level address segments. Defaults to " • ".
  separator?: string;
}

const trim = (value: string | null | undefined): string => (value ?? "").trim();

// formatSiteAddress joins the address segments in display order:
// "<address> • <city, state> • <postal> [• <country>]". Empty
// segments drop out so a half-filled site never renders stray
// separators. Returns "" when nothing is present — callers decide
// whether to render a placeholder ("—") or hide the row entirely.
export const formatSiteAddress = (parts: AddressParts, options: FormatOptions = {}): string => {
  const { includeCountry = false, separator = " • " } = options;
  const cityState = [trim(parts.locationCity), trim(parts.locationState)].filter(Boolean).join(", ");
  const segments = [trim(parts.address), cityState, trim(parts.postalCode)];
  if (includeCountry) segments.push(trim(parts.country));
  return segments.filter(Boolean).join(separator);
};
