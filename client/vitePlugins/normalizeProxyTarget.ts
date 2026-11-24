export const normalizeProxyTarget = (rawTarget: string): string => {
  const trimmedTarget = rawTarget.trim();
  const hasExplicitProtocol = /^[a-z][a-z\d+.-]*:\/\//i.test(trimmedTarget);
  const target = hasExplicitProtocol ? trimmedTarget : `http://${trimmedTarget}`;
  const parsedTarget = new URL(target);

  if (parsedTarget.protocol !== "http:" && parsedTarget.protocol !== "https:") {
    throw new Error("Proxy target must use http:// or https://");
  }

  return target;
};
