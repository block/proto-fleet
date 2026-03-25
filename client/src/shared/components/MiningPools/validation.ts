import { usernameValidationErrors } from "./PoolForm/constants";

interface PoolUsernameValidationOptions {
  required?: boolean;
  disallowSeparator?: boolean;
  allowSeparatorWhenEqualTo?: string;
}

export const getPoolUsernameValidationError = (
  username: string | undefined,
  { required = true, disallowSeparator = false, allowSeparatorWhenEqualTo }: PoolUsernameValidationOptions = {},
) => {
  const normalizedUsername = username?.trim() ?? "";
  const allowedSeparatorUsername = allowSeparatorWhenEqualTo?.trim() ?? "";

  if (!normalizedUsername) {
    return required ? usernameValidationErrors.required : undefined;
  }

  if (disallowSeparator && normalizedUsername.includes(".") && normalizedUsername !== allowedSeparatorUsername) {
    return usernameValidationErrors.separator;
  }

  return undefined;
};
