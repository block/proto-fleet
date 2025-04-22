export const info = {
  url: "url",
  username: "username",
  password: "password",
  priority: "priority",
} as const;

export const emptyPoolInfo = {
  [info.url]: "",
  [info.username]: "",
  [info.password]: "",
  [info.priority]: 0,
};
