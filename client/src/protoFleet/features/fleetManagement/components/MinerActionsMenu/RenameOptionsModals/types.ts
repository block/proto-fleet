import { fixedCharacterCountAll, fixedCharacterCountValues } from "./constants";

export const customPropertyTypes = {
  stringAndCounter: "string-and-counter",
  counterOnly: "counter-only",
  stringOnly: "string-only",
} as const;

export type CustomPropertyType = (typeof customPropertyTypes)[keyof typeof customPropertyTypes];

export const customPropertyTypeLabels: Record<CustomPropertyType, string> = {
  [customPropertyTypes.stringAndCounter]: "Custom string + counter",
  [customPropertyTypes.counterOnly]: "Counter only",
  [customPropertyTypes.stringOnly]: "String only",
};

export interface CustomPropertyOptionsValues {
  type: CustomPropertyType;
  prefix: string;
  suffix: string;
  counterStart?: number;
  counterScale: number;
  stringValue: string;
}

export type FixedCharacterCount = typeof fixedCharacterCountAll | (typeof fixedCharacterCountValues)[number];

export const fixedStringSections = {
  first: "first",
  last: "last",
} as const;

export type FixedStringSection = (typeof fixedStringSections)[keyof typeof fixedStringSections];

export interface FixedValueOptionsValues {
  characterCount: FixedCharacterCount;
  stringSection?: FixedStringSection;
}

export interface QualifierOptionsValues {
  prefix: string;
  suffix: string;
}
