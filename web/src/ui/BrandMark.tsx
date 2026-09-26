export function BrandMark({
  size = 40,
  decorative = false,
}: {
  size?: number;
  decorative?: boolean;
}) {
  return (
    <img
      className="brand-mark"
      src={`${import.meta.env.BASE_URL}brand/convomeow.png`}
      width={size}
      height={size}
      alt={decorative ? "" : "ConvoMeow"}
      draggable={false}
    />
  );
}
