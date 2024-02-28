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

export const tabs = {
  pools: "pools",
  cooling: "cooling",
} as const;

export const fanModes = {
  auto: "auto",
  false: "false",
} as const;
