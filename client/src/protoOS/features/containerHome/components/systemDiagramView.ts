export const SCHEMATIC_WIDTH = 1120;
export const SCHEMATIC_HEIGHT = 600;
export const MAX_ZOOM = 3;
const PADDING = 16;

export interface Point {
  x: number;
  y: number;
}

export interface View extends Point {
  scale: number;
}

export interface ViewportSize {
  width: number;
  height: number;
}

export const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));

export const fitScale = ({ width, height }: ViewportSize) =>
  Math.min((width - PADDING * 2) / SCHEMATIC_WIDTH, (height - PADDING * 2) / SCHEMATIC_HEIGHT, 1.35);

export const minimumScale = (size: ViewportSize) => fitScale(size) / 2;

export function constrainView(view: View, size: ViewportSize): View {
  const scale = clamp(view.scale, minimumScale(size), MAX_ZOOM);
  const axis = (offset: number, content: number, available: number) =>
    content * scale <= available - PADDING * 2
      ? (available - content * scale) / 2
      : clamp(offset, available - PADDING - content * scale, PADDING);
  return { scale, x: axis(view.x, SCHEMATIC_WIDTH, size.width), y: axis(view.y, SCHEMATIC_HEIGHT, size.height) };
}

export const fitView = (size: ViewportSize): View => constrainView({ scale: fitScale(size), x: 0, y: 0 }, size);

export function zoomView(
  view: View,
  size: ViewportSize,
  requestedScale: number,
  anchor: Point = { x: size.width / 2, y: size.height / 2 },
): View {
  const scale = clamp(requestedScale, minimumScale(size), MAX_ZOOM);
  return constrainView(
    {
      scale,
      x: anchor.x - ((anchor.x - view.x) / view.scale) * scale,
      y: anchor.y - ((anchor.y - view.y) / view.scale) * scale,
    },
    size,
  );
}

export function resizeView(view: View, previous: ViewportSize, next: ViewportSize): View {
  if (Math.abs(view.scale - fitScale(previous)) < 0.001) return fitView(next);
  const scale = clamp(view.scale, minimumScale(next), MAX_ZOOM);
  return constrainView(
    {
      scale,
      x: next.width / 2 - ((previous.width / 2 - view.x) / view.scale) * scale,
      y: next.height / 2 - ((previous.height / 2 - view.y) / view.scale) * scale,
    },
    next,
  );
}
