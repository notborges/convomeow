// biome-ignore-all lint/a11y/useMediaCaption: WhatsApp media does not provide caption tracks.

import {
  ArrowExpandIcon,
  Download04Icon,
  File02Icon,
  Image01Icon,
  Loading03Icon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Attachment } from "../../api/types";
import { kindLabel } from "../../i18n/format";
import { AudioPlayer } from "../../ui/AudioPlayer";
import { Button } from "../../ui/Button";
import { ImageViewer } from "../../ui/ImageViewer";
import { useAttachmentMedia } from "./useAttachmentMedia";

export function AttachmentView({
  attachment,
  caption,
  onDimensions,
}: {
  attachment: Attachment;
  caption?: string;
  onDimensions?: (size: { width: number; height: number }) => void;
}) {
  const { t } = useTranslation();

  const [viewerOpen, setViewerOpen] = useState(false);
  const [imageFailed, setImageFailed] = useState(false);
  const [imageLoaded, setImageLoaded] = useState(false);
  const {
    container,
    current,
    activated,
    activate,
    requested,
    failure: downloadFailure,
    contentURL,
    download,
  } = useAttachmentMedia(attachment);
  const failure = imageFailed ? "mediaLoad" : downloadFailure;
  const inlineImage = current.kind === "image" || current.kind === "sticker";
  const [naturalSize, setNaturalSize] = useState<{
    width: number;
    height: number;
  }>();
  const dimensions =
    current.width && current.height
      ? { width: current.width, height: current.height }
      : naturalSize;
  const ratio = dimensions ? dimensions.width / dimensions.height : undefined;
  useEffect(() => {
    if (current.width && current.height)
      onDimensions?.({ width: current.width, height: current.height });
  }, [current.width, current.height, onDimensions]);
  async function requestDownload() {
    if (await download()) setImageFailed(false);
  }

  function content() {
    if (current.kind === "audio")
      return (
        <AudioPlayer
          src={
            current.availability === "ready" && activated
              ? contentURL
              : undefined
          }
          name={current.file_name}
          durationSeconds={current.duration_seconds}
          loading={requested}
          unavailable={current.availability === "unavailable"}
          error={
            current.availability !== "ready" && failure && !requested
              ? t(($) => $.errors[failure])
              : undefined
          }
          onRequest={() => {
            activate();
            void requestDownload();
          }}
        />
      );
    if (current.availability === "ready" && !imageFailed && activated) {
      if (current.kind === "image" || current.kind === "sticker")
        return (
          <button
            type="button"
            className={`attachment attachment--image ${current.kind === "sticker" ? "attachment--sticker" : ""}`}
            onClick={() => setViewerOpen(true)}
            aria-label={t(($) => $.viewer.open)}
            disabled={!imageLoaded}
          >
            <img
              src={contentURL}
              alt={current.file_name || t(($) => $.media.image)}
              loading="eager"
              className={imageLoaded ? "is-loaded" : ""}
              onLoad={(event) => {
                const size = {
                  width: event.currentTarget.naturalWidth,
                  height: event.currentTarget.naturalHeight,
                };
                setNaturalSize(size);
                onDimensions?.(size);
                setImageLoaded(true);
              }}
              onError={() => {
                setImageFailed(true);
              }}
            />
            {!imageLoaded && (
              <span className="attachment__loading" role="status">
                <HugeiconsIcon
                  icon={Loading03Icon}
                  size={22}
                  className="spin"
                />
                <span className="sr-only">{t(($) => $.viewer.loading)}</span>
              </span>
            )}
            <span
              className="attachment__expand"
              aria-hidden="true"
              hidden={!imageLoaded}
            >
              <HugeiconsIcon icon={ArrowExpandIcon} size={18} />
            </span>
          </button>
        );
      if (current.kind === "video")
        return (
          <video
            className="attachment attachment--video"
            src={contentURL}
            controls
            preload="none"
            aria-label={current.file_name || t(($) => $.media.video)}
          />
        );
      return (
        <a
          className="attachment attachment--file"
          href={contentURL}
          download={current.file_name || t(($) => $.media.fileName)}
        >
          <HugeiconsIcon icon={File02Icon} size={20} />
          <span>{current.file_name || t(($) => $.media.document)}</span>
          <HugeiconsIcon icon={Download04Icon} size={18} />
        </a>
      );
    }

    if (current.availability === "unavailable")
      return (
        <div
          className={`attachment attachment--pending ${inlineImage ? "attachment--visual" : ""}`}
        >
          <HugeiconsIcon icon={Image01Icon} size={19} />
          <span>{t(($) => $.media.unavailable)}</span>
        </div>
      );
    if (inlineImage && (!failure || requested))
      return (
        <div
          className="attachment attachment--pending attachment--visual"
          role="status"
        >
          <HugeiconsIcon icon={Loading03Icon} size={22} className="spin" />
          <span className="sr-only">{t(($) => $.viewer.loading)}</span>
        </div>
      );
    return (
      <div
        className={`attachment attachment--pending ${inlineImage ? "attachment--visual" : ""}`}
      >
        {requested ? (
          <HugeiconsIcon icon={Loading03Icon} size={18} className="spin" />
        ) : (
          <HugeiconsIcon icon={Download04Icon} size={18} />
        )}
        <div>
          <span>{current.file_name || kindLabel(current.kind)}</span>
          {failure && <small role="alert">{t(($) => $.errors[failure])}</small>}
        </div>
        <Button
          variant="text"
          type="button"
          onClick={requestDownload}
          disabled={requested}
        >
          {requested
            ? t(($) => $.media.fetching)
            : failure
              ? t(($) => $.common.retry)
              : t(($) => $.media.download)}
        </Button>
      </div>
    );
  }
  return (
    <div
      ref={container}
      className={`attachment-container ${inlineImage ? "attachment-container--visual" : ""}`}
      style={inlineImage && ratio ? { aspectRatio: ratio } : undefined}
      aria-busy={requested || undefined}
    >
      {content()}
      {viewerOpen && (
        <ImageViewer
          src={contentURL}
          name={current.file_name}
          caption={caption}
          onClose={() => setViewerOpen(false)}
        />
      )}
    </div>
  );
}
