export function generateRandomText(prefix: string): string {
  const randomCode = Math.random().toString(36).substring(2, 9);
  return `${prefix}_${randomCode}`;
}

export function generateRandomUsername(): string {
  return generateRandomText("username");
}
