import resolveConfig from "tailwindcss/resolveConfig";
import config from "../../../tailwind.config";

const tailwindConfig = resolveConfig(config);

// Type to get the type of an array element
type ArrayElement<T> = T extends Array<infer U> ? U : never;

// Type to get nested property types
type PathValue<T, P extends any[]> = P extends []
  ? T
  : P extends [infer First, ...infer Rest]
    ? First extends keyof T
      ? PathValue<T[First], Rest>
      : First extends number
        ? T extends Array<any>
          ? PathValue<ArrayElement<T>, Rest>
          : undefined
        : undefined
    : never;

export default function getTailwindConfig<
  T extends typeof tailwindConfig,
  P extends (string | number)[],
>(...path: P): PathValue<T, P> {
  if (!path.length) {
    return tailwindConfig as PathValue<T, P>;
  }

  let current: unknown = tailwindConfig;
  for (const key of path) {
    if (current == null || typeof current !== "object") {
      return undefined as PathValue<T, P>;
    }

    const obj = current as { [key: string | number]: unknown };
    if (!(key in obj)) {
      return undefined as PathValue<T, P>;
    }

    current = obj[key];
  }

  return current as PathValue<T, P>;
}
