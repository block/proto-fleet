interface ObjectCoverSourceCropInput {
  sourceWidth: number;
  sourceHeight: number;
  renderedWidth: number;
  renderedHeight: number;
}

interface ObjectCoverRegionSourceCropInput extends ObjectCoverSourceCropInput {
  renderedRegionX: number;
  renderedRegionY: number;
  renderedRegionWidth: number;
  renderedRegionHeight: number;
}

export interface SourceCrop {
  sx: number;
  sy: number;
  sw: number;
  sh: number;
}

/**
 * Return the source rectangle visible when an intrinsic image/video is rendered
 * with CSS `object-fit: cover` into the given box.
 */
export function getObjectCoverSourceCrop({
  sourceWidth,
  sourceHeight,
  renderedWidth,
  renderedHeight,
}: ObjectCoverSourceCropInput): SourceCrop | null {
  if (!sourceWidth || !sourceHeight || !renderedWidth || !renderedHeight) return null;

  const sourceAspect = sourceWidth / sourceHeight;
  const renderedAspect = renderedWidth / renderedHeight;

  if (sourceAspect > renderedAspect) {
    const sw = sourceHeight * renderedAspect;
    return {
      sx: (sourceWidth - sw) / 2,
      sy: 0,
      sw,
      sh: sourceHeight,
    };
  }

  if (sourceAspect < renderedAspect) {
    const sh = sourceWidth / renderedAspect;
    return {
      sx: 0,
      sy: (sourceHeight - sh) / 2,
      sw: sourceWidth,
      sh,
    };
  }

  return {
    sx: 0,
    sy: 0,
    sw: sourceWidth,
    sh: sourceHeight,
  };
}

/**
 * Return the source rectangle corresponding to a rendered sub-region inside an
 * `object-fit: cover` box.
 */
export function getObjectCoverSourceCropForRegion({
  sourceWidth,
  sourceHeight,
  renderedWidth,
  renderedHeight,
  renderedRegionX,
  renderedRegionY,
  renderedRegionWidth,
  renderedRegionHeight,
}: ObjectCoverRegionSourceCropInput): SourceCrop | null {
  const visibleCrop = getObjectCoverSourceCrop({ sourceWidth, sourceHeight, renderedWidth, renderedHeight });
  if (!visibleCrop) return null;

  const left = clamp(renderedRegionX, 0, renderedWidth);
  const top = clamp(renderedRegionY, 0, renderedHeight);
  const right = clamp(renderedRegionX + renderedRegionWidth, 0, renderedWidth);
  const bottom = clamp(renderedRegionY + renderedRegionHeight, 0, renderedHeight);

  if (right <= left || bottom <= top) return null;

  const scaleX = visibleCrop.sw / renderedWidth;
  const scaleY = visibleCrop.sh / renderedHeight;

  return {
    sx: visibleCrop.sx + left * scaleX,
    sy: visibleCrop.sy + top * scaleY,
    sw: (right - left) * scaleX,
    sh: (bottom - top) * scaleY,
  };
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}
