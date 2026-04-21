/** Maximum number of pools that can be configured */
export const MAX_POOLS = 3;

export const poolInfoAttributes = {
  name: "name",
  url: "url",
  username: "username",
  password: "password",
  priority: "priority",
} as const;

export const emptyPoolInfo = {
  [poolInfoAttributes.name]: "",
  [poolInfoAttributes.url]: "",
  [poolInfoAttributes.username]: "",
  [poolInfoAttributes.password]: "",
  [poolInfoAttributes.priority]: 0,
};
