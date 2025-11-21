/**
 * Formats database role names into user-friendly display names
 */
export const formatRole = (role: string): string => {
  const roleMap: Record<string, string> = {
    SUPER_ADMIN: "Owner",
    ADMIN: "Admin",
  };

  return roleMap[role] || role;
};
