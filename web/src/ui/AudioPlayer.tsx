// biome-ignore-all lint/a11y/useMediaCaption: Audio messages do not include caption tracks.
import { PauseIcon, PlayIcon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, IconButton } from "./Button";

// Claim playback before fetching so delayed audio cannot interrupt a newer selection.
let playbackOwner: HTMLAudioElement | undefined;

function timestamp(seconds: number) {
  if (!Number.isFinite(seconds)) return "0:00";
  return `${Math.floor(seconds / 60)}:${Math.floor(seconds % 60)
    .toString()
    .padStart(2, "0")}`;
}

export function AudioPlayer({
  src,
  name,
  durationSeconds,
  loading = false,
  unavailable = false,
  error,
  onRequest,
}: {
  src?: string;
  name?: string;
  durationSeconds?: number;
  loading?: boolean;
  unavailable?: boolean;
  error?: string;
  onRequest?: () => void;
}) {
  const { t } = useTranslation();
  const audio = useRef<HTMLAudioElement>(null);
  const [wanted, setWanted] = useState(false);
  const [playing, setPlaying] = useState(false);
  const [buffering, setBuffering] = useState(false);
  const [failed, setFailed] = useState(false);
  const [position, setPosition] = useState(0);
  const [measuredDuration, setDuration] = useState(0);
  const duration = measuredDuration || durationSeconds || 0;
  const [speed, setSpeed] = useState(1);
  useEffect(() => {
    const node = audio.current;
    return () => {
      node?.pause();
      if (playbackOwner === node) playbackOwner = undefined;
    };
  }, []);
  useEffect(() => {
    if (error || unavailable) setWanted(false);
  }, [error, unavailable]);
  useEffect(() => {
    const node = audio.current;
    if (!wanted || !src || !node) return;
    setWanted(false);
    if (playbackOwner === node) startPlayback();
  }, [wanted, src]);
  function startPlayback() {
    const node = audio.current;
    if (!node) return;
    setBuffering(true);
    void node.play().catch((cause: unknown) => {
      setBuffering(false);
      if (
        !(
          cause instanceof DOMException &&
          (cause.name === "NotAllowedError" || cause.name === "AbortError")
        )
      )
        setFailed(true);
    });
  }
  useEffect(() => {
    if (!buffering) return;
    const timer = setTimeout(() => {
      audio.current?.pause();
      setBuffering(false);
      setFailed(true);
    }, 15000);
    return () => clearTimeout(timer);
  }, [buffering]);
  const busy = loading || buffering;
  const failure = error || (failed ? t(($) => $.audio.failed) : undefined);
  return (
    <section
      className="audio-player"
      aria-label={name || t(($) => $.media.audio)}
    >
      <audio
        ref={audio}
        src={src}
        preload={durationSeconds ? "none" : "metadata"}
        onLoadedMetadata={(event) => {
          event.currentTarget.playbackRate = speed;
        }}
        onPlaying={() => {
          if (playbackOwner !== audio.current) {
            audio.current?.pause();
            return;
          }
          setPlaying(true);
          setBuffering(false);
        }}
        onPause={() => {
          setPlaying(false);
          setBuffering(false);
        }}
        onWaiting={() => setBuffering(true)}
        onEnded={() => {
          setPlaying(false);
          setBuffering(false);
        }}
        onTimeUpdate={(event) => setPosition(event.currentTarget.currentTime)}
        onDurationChange={(event) =>
          setDuration(
            Number.isFinite(event.currentTarget.duration)
              ? event.currentTarget.duration
              : 0,
          )
        }
        onError={() => {
          setFailed(true);
          setBuffering(false);
          setPlaying(false);
        }}
      />
      <div className="audio-player__controls">
        <IconButton
          className="audio-player__toggle"
          variant="primary"
          label={
            busy
              ? t(($) => $.audio.loading)
              : playing
                ? t(($) => $.audio.pause)
                : failure
                  ? t(($) => $.audio.retry)
                  : t(($) => $.audio.play)
          }
          busy={busy}
          disabled={unavailable}
          onClick={() => {
            if (playing) {
              audio.current?.pause();
              return;
            }
            if (playbackOwner && playbackOwner !== audio.current)
              playbackOwner.pause();
            playbackOwner = audio.current ?? undefined;
            if (failed) audio.current?.load();
            setFailed(false);
            if (src) startPlayback();
            else {
              setWanted(true);
              onRequest?.();
            }
          }}
        >
          <HugeiconsIcon icon={playing ? PauseIcon : PlayIcon} size={20} />
        </IconButton>
        <div className="audio-player__timeline">
          <input
            type="range"
            min={0}
            max={duration || 1}
            step={0.1}
            value={position}
            disabled={!measuredDuration || unavailable}
            aria-label={t(($) => $.audio.seek)}
            aria-valuetext={`${timestamp(position)} / ${timestamp(duration)}`}
            onChange={(event) => {
              const value = Number(event.target.value);
              if (audio.current) audio.current.currentTime = value;
              setPosition(value);
            }}
          />
          <span>
            {timestamp(position)} / {duration ? timestamp(duration) : "—:—"}
          </span>
        </div>
        <Button
          variant="ghost"
          className="audio-player__speed"
          aria-label={t(($) => $.audio.speed, { speed })}
          onClick={() => {
            const next = speed === 2 ? 1 : speed + 0.5;
            setSpeed(next);
            if (audio.current) audio.current.playbackRate = next;
          }}
        >
          {speed}×
        </Button>
      </div>
      {unavailable ? (
        <p>{t(($) => $.media.unavailable)}</p>
      ) : failure && !busy ? (
        <p role="alert">{failure}</p>
      ) : null}
    </section>
  );
}
