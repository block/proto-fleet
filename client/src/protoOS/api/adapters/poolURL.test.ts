import { expect, test } from "vitest";
import { validateContainerPoolURL, validateMiningGroupPoolURL } from "./poolURL";

test("hosted mining groups accept only credential-free Stratum TCP addresses", () => {
  expect(validateMiningGroupPoolURL("pool.example:3333")).toBeUndefined();
  expect(validateMiningGroupPoolURL("tcp://pool.example:3333")).toBeUndefined();
  expect(validateMiningGroupPoolURL("stratum+ssl://pool.example:3333")).toMatch("stratum+tcp");
  expect(validateMiningGroupPoolURL("stratum+tcp://user:secret@pool.example:3333")).toMatch("valid pool host");
  expect(validateMiningGroupPoolURL("stratum+tcp://pool.example:3333/path?token=secret")).toMatch("valid pool host");
  expect(validateMiningGroupPoolURL("tcp://pool.example:3333?")).toMatch("valid pool host");
  expect(validateMiningGroupPoolURL("pool.example:3333#")).toMatch("valid pool host");
});

test("the Default configuration keeps the legacy controller schemes without accepting URL credentials", () => {
  expect(validateContainerPoolURL("stratum+ssl://pool.example:3333")).toBeUndefined();
  expect(validateContainerPoolURL("datum://pool.example:28915")).toBeUndefined();
  expect(validateContainerPoolURL("datum://user:secret@pool.example:28915")).toMatch("valid pool host");
});
