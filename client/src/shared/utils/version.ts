/**
 * Application version information
 */

export interface BuildVersionInfo {
  /** Application version (e.g. v1.0.0) */
  version: string;
  /** Build date in ISO format */
  buildDate: string;
  /** Git commit hash */
  commit: string;
}

export const buildVersionInfo: BuildVersionInfo = {
  version: import.meta.env.VITE_VERSION || "dev",
  buildDate: import.meta.env.VITE_BUILD_DATE || "unknown",
  commit: import.meta.env.VITE_COMMIT || "local",
};

export const logBuildVersion = () => {
  const { version, buildDate, commit } = buildVersionInfo;

  // eslint-disable-next-line no-console
  console.log(`%cVersion %c${version}`, "font-weight: bold; color: #333;", "font-weight: bold; color: #0078D7;");

  // eslint-disable-next-line no-console
  console.log(`Built on: ${buildDate} (${commit})`);
};

export default buildVersionInfo;
