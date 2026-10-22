export const INVITATION_CODE_GROUP_SIZE = 4;
export const INVITATION_CODE_GROUP_COUNT = 2;
export const INVITATION_CODE_LENGTH = INVITATION_CODE_GROUP_SIZE * INVITATION_CODE_GROUP_COUNT;
export const INVITATION_CODE_FORMAT = Array.from({ length: INVITATION_CODE_GROUP_COUNT }, () =>
  "X".repeat(INVITATION_CODE_GROUP_SIZE),
).join("-");

// Matches the server alphabet, which omits 0, 1, I and O.
export const isInvitationCodeCharacter = (character: string) => /^[A-HJ-NP-Z2-9]$/.test(character);

/** Uppercases and drops the ASCII spaces and hyphens that people copy along with a code. */
export const normalizeInvitationCode = (value: string) => value.replace(/[ -]/g, "").toUpperCase();

export const emptyInvitationCode = () => Array<string>(INVITATION_CODE_LENGTH).fill("");

export const invitationCodeFrom = (value: string) => {
  const characters = [...normalizeInvitationCode(value)].slice(0, INVITATION_CODE_LENGTH);
  return emptyInvitationCode().map((_, index) => characters[index] ?? "");
};

export const isCompleteInvitationCode = (characters: string[]) =>
  characters.length === INVITATION_CODE_LENGTH && characters.every(isInvitationCodeCharacter);

/** Canonical XXXX-XXXX-… form sent to the server. */
export const formatInvitationCode = (characters: string[]) =>
  Array.from({ length: INVITATION_CODE_GROUP_COUNT }, (_, group) =>
    characters.slice(group * INVITATION_CODE_GROUP_SIZE, (group + 1) * INVITATION_CODE_GROUP_SIZE).join(""),
  ).join("-");
