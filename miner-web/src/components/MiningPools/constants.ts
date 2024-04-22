export const info = {
  url: "url",
  username: "username",
  password: "password",
} as const;

export const emptyPoolInfo = {
  [info.url]: "",
  [info.username]: "",
  [info.password]: "",
};
