export function generateRandomUsername(prefix: string = "member"): string {
  const randomCode = Math.random().toString(36).substring(2, 9);
  return `${prefix}${randomCode}`;
}
