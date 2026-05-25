// extracts cubic bezier values from a css string so that it could be passed to motion/react
export const cubicBezierValues = (string: string) => {
  const cbMatch = string.match(/cubic-bezier\((.*)\)/);
  if (!cbMatch) return undefined;

  const cbString = cbMatch[0];
  const values = cbString
    .replace("cubic-bezier(", "")
    .replace(")", "")
    .split(",")
    .map((value) => parseFloat(value));

  return values.length == 4 ? values : undefined;
};

const escapeCssAttributeValue = (value: string): string => value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');

export const classNameToSelectors = (className: string): string[] => {
  return className
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((classNameToken) => `[class~="${escapeCssAttributeValue(classNameToken)}"]`);
};
