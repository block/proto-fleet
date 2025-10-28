import {
  MutableRefObject,
  RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

const useUnitOffset = (
  value: string | number,
  inputRef: RefObject<HTMLInputElement | null>,
  units?: string,
) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(
    null,
  ) as MutableRefObject<HTMLCanvasElement | null>;
  const [context, setContext] = useState<CanvasRenderingContext2D | null>(null);
  const [valueWidth, setValueWidth] = useState<number>();

  // create a canvas element to measure text width
  // if units were not defined no need to create a canvas
  useEffect(() => {
    if (!units) return;
    canvasRef.current = document.createElement("canvas");
    setContext(canvasRef.current.getContext("2d"));

    return () => {
      canvasRef.current = null;
      setContext(null);
    };
  }, [units]);

  // render text to canvas and measure
  const updateUnitPosition = useCallback(() => {
    const input = inputRef.current;
    if (!input || !context || value === undefined) return;

    const inputStyle = window.getComputedStyle(input);
    context.font = inputStyle.font;
    setValueWidth(context.measureText(input.value).width);
  }, [context, inputRef, value]);

  useEffect(() => {
    updateUnitPosition();
  }, [value, context, updateUnitPosition]);

  // recompute on resize
  useEffect(() => {
    window.addEventListener("resize", updateUnitPosition);

    return () => {
      window.removeEventListener("resize", updateUnitPosition);
    };
  }, [updateUnitPosition]);

  return valueWidth;
};

export default useUnitOffset;
