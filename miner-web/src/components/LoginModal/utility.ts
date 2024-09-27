export const accessTokenExpiryTime = () => {
  // 30 minutes
  return new Date(new Date().getTime() + 30 * 60 * 1000);
};

export const refreshTokenExpiryTime = () => {
  // 15 days
  return new Date(new Date().getTime() + 15 * 24 * 60 * 60 * 1000);
};
