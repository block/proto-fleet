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
 * - `/sites` and `/buildings/:id` routes are discoverable via nav entry points.
 * - The topbar SitePicker replaces the placeholder LocationSelector.
 * Override with `VITE_MULTI_SITE_ENABLED=true`.
 */
export const MULTI_SITE_ENABLED = import.meta.env.VITE_MULTI_SITE_ENABLED === "true";

/**
 * Notifications settings (webhook/Slack delivery channels). When on, the
 * `/settings/notifications` entry is discoverable in the settings subnav.
 *
 * Notifications require the Grafana sidecar, which only runs when the server is
 * started with notifications enabled (`ENABLE_BETA_NOTIFICATIONS=true` →
 * `just dev-notifs`). With the sidecar absent the page can't load anything, so
 * the nav stays hidden by default. Override with `VITE_NOTIFICATIONS_ENABLED=true`.
 */
export const NOTIFICATIONS_ENABLED = import.meta.env.VITE_NOTIFICATIONS_ENABLED === "true";
