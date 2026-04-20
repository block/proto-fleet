/**
 * Returns new object with only specified keys
 *
 * @param obj - object to pick keys from
 * @param keys - keys to pick
 * @returns new object with only specified keys
 */
export const pick = <T extends object, K extends keyof T>(obj: T, keys: K[]): Pick<T, K> => {
  const result = {} as Pick<T, K>;
  keys.forEach((key) => {
    if (obj[key] !== undefined) {
      result[key] = obj[key];
    }
  });
  return result;
};

/**
 * Returns new object, omitting specified keys
 *
 * @param obj - object to omit keys from
 * @param keys - keys to omit
 * @returns new object with specified keys omitted
 */
export const omit = <T extends object, K extends keyof T>(obj: T, keys: K[]): Omit<T, K> => {
  const result = { ...obj };
  keys.forEach((key) => {
    delete result[key];
  });
  return result;
};
