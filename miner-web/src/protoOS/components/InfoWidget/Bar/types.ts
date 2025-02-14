export type ColorKey = "blue" | "green" | "orange" | "redOrange" | "red";

interface ColorTypes {
  bg: string;
  gradient: string;
  id: string;
}

export type Colors = {
  [key in ColorKey]: ColorTypes;
};
