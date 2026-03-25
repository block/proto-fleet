export const poolNameValidationErrors = {
  required: "A Pool Name is required.",
} as const;

export const urlValidationErrors = {
  required: "A Pool URL is required to connect to this pool.",
  duplicate: "This Pool URL and Username combination is already configured.",
} as const;

export const usernameValidationErrors = {
  required: "A Username is required to connect to this pool.",
  separator: "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
} as const;

export const fleetUsernameHelperText = "Worker name will be appended to this username when applied to miners.";
