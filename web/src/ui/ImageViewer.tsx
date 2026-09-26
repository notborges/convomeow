import {
  Download04Icon,
  ZoomInIcon,
  ZoomOutIcon,
} from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, IconButton } from "./Button";
import { Dialog } from "./Dialog";
import { Loading } from "./Loading";

export function ImageViewer({
  src,
  name,
  caption,
  onClose,
}: {
  src: string;
  name?: string;
  caption?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const viewport = useRef<HTMLElement>(null);
  const [bounds, setBounds] = useState({ width: 0, height: 0 });
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [zoom, setZoom] = useState(1);
  const [failed, setFailed] = useState(false);
  const fit = size.width
    ? Math.min(1, bounds.width / size.width, bounds.height / size.height)
    : 0;

  useLayoutEffect(() => {
    const node = viewport.current;
    if (!node) return;
    const measure = () =>
      setBounds({
        width: Math.max(1, node.clientWidth - 32),
        height: Math.max(1, node.clientHeight - 32),
      });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const node = viewport.current;
    if (node) {
      node.scrollLeft = (node.scrollWidth - node.clientWidth) / 2;
      node.scrollTop = (node.scrollHeight - node.clientHeight) / 2;
    }
  }, [zoom, fit]);

  return (
    <Dialog
      title={name || t(($) => $.viewer.title)}
      onClose={onClose}
      className="image-viewer"
      actions={
        <a
          className="button button--ghost icon-button"
          href={src}
          download={name || t(($) => $.media.fileName)}
          aria-label={t(($) => $.media.download)}
          title={t(($) => $.media.download)}
        >
          <HugeiconsIcon icon={Download04Icon} size={20} />
        </a>
      }
    >
      <section
        ref={viewport}
        className="image-viewer__viewport"
        // biome-ignore lint/a11y/noNoninteractiveTabindex: Keyboard focus enables scrolling a zoomed image.
        tabIndex={0}
        aria-label={t(($) => $.viewer.title)}
      >
        {failed ? (
          <p className="image-viewer__error" role="alert">
            {t(($) => $.viewer.failed)}
          </p>
        ) : (
          <>
            {!size.width && <Loading label={t(($) => $.viewer.loading)} />}
            <div className="image-viewer__canvas">
              <img
                className="image-viewer__image"
                src={src}
                alt={caption || name || t(($) => $.viewer.title)}
                style={{
                  width: size.width ? size.width * fit * zoom : undefined,
                  height: size.height ? size.height * fit * zoom : undefined,
                  visibility: size.width ? "visible" : "hidden",
                }}
                onLoad={(event) =>
                  setSize({
                    width: event.currentTarget.naturalWidth,
                    height: event.currentTarget.naturalHeight,
                  })
                }
                onError={() => setFailed(true)}
              />
            </div>
          </>
        )}
      </section>
      <footer className="image-viewer__footer">
        {caption && <p>{caption}</p>}
        <div className="image-viewer__zoom">
          <IconButton
            label={t(($) => $.viewer.zoomOut)}
            disabled={!size.width || failed || zoom === 1}
            onClick={() => setZoom((value) => Math.max(1, value - 0.5))}
          >
            <HugeiconsIcon icon={ZoomOutIcon} size={20} />
          </IconButton>
          <Button
            variant="ghost"
            disabled={!size.width || failed}
            onClick={() => setZoom(1)}
          >
            {t(($) => $.viewer.fit)}
          </Button>
          <IconButton
            label={t(($) => $.viewer.zoomIn)}
            disabled={!size.width || failed || zoom === 4}
            onClick={() => setZoom((value) => Math.min(4, value + 0.5))}
          >
            <HugeiconsIcon icon={ZoomInIcon} size={20} />
          </IconButton>
        </div>
      </footer>
    </Dialog>
  );
}
