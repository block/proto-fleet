export const getAuthHeader = (accessToken: string) => ({
  headers: { Authorization: `Bearer ${accessToken}` },
});
