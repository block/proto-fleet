import { Values } from "@/shared/components/Setup/authentication.types";

export const minPasswordLength = 8;
export const weakPasswordThreshold = 50;

export const passwordErrors = {
  tooShort: `Minimum ${minPasswordLength} characters required`,
  mismatch: "Passwords don't match",
  required: "A password is required",
  usernameRequired: "A username is required",
  currentPasswordRequired: "Current password is required",
} as const;

export const isPasswordTooShort = (password: string): boolean => {
  return password.length < minPasswordLength;
};

export const isWeakPassword = (score: number): boolean => {
  return score < weakPasswordThreshold;
};

export const initValues: Values = {
  username: "",
  currentPassword: "",
  password: "",
  confirmPassword: "",
};

export const initErrors: Values = {
  username: "",
  currentPassword: "",
  password: "",
  confirmPassword: "",
};
