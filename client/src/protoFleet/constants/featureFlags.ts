/**
 * Build-time feature flags for ProtoFleet. Each flag is parsed once at
 * module load from a Vite env var; the default for any unset flag is
 * `false` so forgetting the env var is the safer failure mode.
 *
 * Flags gate nav entries and standalone UI elements — they do not gate
 * routes themselves, so direct-URL access remains available for QA and
 * dogfood while a feature is in development.
 */

/**
 * Multi-site UI. When on:
 * - `/sites`, `/settings/sites`, `/buildings/:id` routes are
 *   discoverable via the sidenav and settings subnav.
 * - The topbar SitePicker replaces the placeholder LocationSelector.
 * Override with `VITE_MULTI_SITE_ENABLED=true`.
 */
export const MULTI_SITE_ENABLED = import.meta.env.VITE_MULTI_SITE_ENABLED === "true";

/**
 * Fleet node management UI. When on, the `/fleet-nodes` nav entry is
 * discoverable in the sidenav. The route itself stays reachable via direct
 * URL regardless of the flag. Override with `VITE_FLEETNODE_ENABLED=true`.
 */
export const FLEETNODE_ENABLED = import.meta.env.VITE_FLEETNODE_ENABLED === "true";
