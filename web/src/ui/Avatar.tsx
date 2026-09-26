import { useState } from "react";

interface AvatarProps {
  name: string;
  url?: string;
  size?: "small" | "medium" | "large";
  className?: string;
}

export function Avatar({
  name,
  url,
  size = "medium",
  className = "",
}: AvatarProps) {
  const [failedURL, setFailedURL] = useState<string>();
  const initials =
    name
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join("") || "?";
  const tone =
    [...name].reduce((value, character) => value + character.charCodeAt(0), 0) %
    6;
  return (
    <span
      className={`avatar avatar--${size} ${className}`}
      data-tone={tone}
      aria-hidden="true"
    >
      {url && failedURL !== url ? (
        <img src={url} alt="" onError={() => setFailedURL(url)} />
      ) : (
        <span>{initials}</span>
      )}
    </span>
  );
}
