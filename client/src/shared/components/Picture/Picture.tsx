export type ImageMetadata = {
  src: string;
  src2x?: string;
  width: number;
  height: number;
};

type PictureProps = {
  image: ImageMetadata | string;
  alt?: string;
  className?: string;
};

const Picture = ({ image, alt, className }: PictureProps) => {
  // if image was not processed by vite plugin the import will be a string
  if (typeof image === "string") {
    return <img src={image} alt={alt} />;
  }

  const { src, src2x, width, height } = image;
  const srcSet = src2x ? `${src} 1x, ${src2x} 2x` : src;
  return (
    <picture>
      <source media="max-device-pixel-ratio: 1.5" srcSet={srcSet} />
      <img src={src} alt={alt || ""} width={width} height={height} className={className} />
    </picture>
  );
};

export default Picture;
