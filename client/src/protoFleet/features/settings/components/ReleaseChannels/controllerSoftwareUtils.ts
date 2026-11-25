import type { UpdateStatus } from "./rolloutStatus";
import { type FrigatePackage, FrigatePackageKind } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import type {
  ReleaseChannelContainer,
  ReleaseChannelControllerSoftware,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// The row names the artifact, as the firmware rows beneath it do.
export const CONTROLLER_SOFTWARE_COMPONENT = "Controller software";

// A channel names the controller software version its containers should run.
// Nothing installs it from here: operators update each container themselves,
// and a container that is behind takes no firmware from the channel until they do.
export interface ControllerSoftwareApi {
  // Setting the version takes both the firmware and the controller software
  // permission. Everyone who can manage the channel can read it.
  canSet: boolean;
  list: (channelId: bigint, signal?: AbortSignal) => Promise<ReleaseChannelControllerSoftware[]>;
  // An empty version clears it.
  set: (channelId: bigint, manufacturer: string, model: string, version: string) => Promise<void>;
  listContainers: (
    channelId: bigint,
    manufacturer: string,
    model: string,
    signal?: AbortSignal,
  ) => Promise<ReleaseChannelContainer[]>;
  // Versions of the uploaded controller software packages.
  listVersions: (signal?: AbortSignal) => Promise<string[]>;
}

/** The versions a channel can name. The server refuses a ProtoOS package version as the controller software target. */
export const controllerSoftwareVersions = (packages: readonly FrigatePackage[]): string[] =>
  packages.filter((item) => item.kind === FrigatePackageKind.CONTROLLER_SOFTWARE).map((item) => item.version);

// Versions compare as the server compares them: trimmed, without one leading "v".
export const controllerSoftwareVersionKey = (version: string): string => version.trim().replace(/^[vV]/, "");

/** Containers that do not report the version the channel names; none when it names no version. */
export const containersBehind = (software: ReleaseChannelControllerSoftware | undefined): number =>
  software && software.targetVersion !== "" ? Math.max(0, software.containerCount - software.onTargetCount) : 0;

export const controllerSoftwareStatus = (software: ReleaseChannelControllerSoftware): UpdateStatus | null => {
  if (software.targetVersion === "" || software.containerCount === 0) return null;
  const behind = containersBehind(software);
  return behind === 0
    ? { label: `All on ${software.targetVersion}`, tone: "completed" }
    : {
        label: `${software.onTargetCount.toLocaleString()} of ${software.containerCount.toLocaleString()} on ${software.targetVersion}`,
        tone: "attention",
      };
};

/** Why a container model's firmware update is waiting, or null when no container is behind. */
export const controllerSoftwareHoldMessage = (
  software: ReleaseChannelControllerSoftware | undefined,
): string | null => {
  const behind = containersBehind(software);
  if (!software || behind === 0) return null;
  return behind === 1
    ? `1 container is held until its controller software is ${software.targetVersion}.`
    : `${behind.toLocaleString()} containers are held until their controller software is ${software.targetVersion}.`;
};

/** Picker options: no version, each uploaded version once, and the current one even if its package is gone. */
export function controllerSoftwareOptions(
  versions: readonly string[],
  targetVersion: string,
): { value: string; label: string; description?: string }[] {
  const options: { value: string; label: string; description?: string }[] = [{ value: "", label: "Not set" }];
  const seen = new Set<string>();
  for (const version of versions) {
    const key = controllerSoftwareVersionKey(version);
    if (key === "" || seen.has(key)) continue;
    seen.add(key);
    options.push({ value: version.trim(), label: version.trim() });
  }
  if (targetVersion !== "" && !seen.has(controllerSoftwareVersionKey(targetVersion))) {
    options.push({ value: targetVersion, label: targetVersion, description: "Package is no longer uploaded" });
  }
  return options;
}

/** The option that stands for the current version, whichever spelling the package uses. */
export const controllerSoftwareOptionValue = (options: readonly { value: string }[], targetVersion: string): string =>
  options.find((option) => controllerSoftwareVersionKey(option.value) === controllerSoftwareVersionKey(targetVersion))
    ?.value ?? targetVersion;
