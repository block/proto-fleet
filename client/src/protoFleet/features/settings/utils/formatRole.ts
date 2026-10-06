/**
 * Formats database role names into user-friendly display names
 */
export const formatRole = (role: string): string => {
  const roleMap: Record<string, string> = {
    SUPER_ADMIN: "Owner",
    ADMIN: "Admin",
    FIELD_TECH: "Field Tech",
  };

  return Object.prototype.hasOwnProperty.call(roleMap, role) ? roleMap[role] : role;
};
