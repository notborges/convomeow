import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Attachment } from "../../api/types";
import { type ErrorKey, errorKey } from "../../i18n/errors";

export function useAttachmentMedia(attachment: Attachment) {
  const [requested, setRequested] = useState(false);
  const [visible, setVisible] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const autoRequested = useRef(false);
  const [activated, setActivated] = useState(false);
  useEffect(() => {
    const node = container.current;
    if (!node) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const observer = new IntersectionObserver(
      ([entry]) => {
        clearTimeout(timer);
        if (!entry.isIntersecting) {
          setVisible(false);
          return;
        }
        setActivated(true);
        timer = setTimeout(() => setVisible(true), 300);
      },
      { root: node.closest(".thread-messages"), threshold: 0.01 },
    );
    observer.observe(node);
    return () => {
      clearTimeout(timer);
      observer.disconnect();
    };
  }, []);
  const [error, setError] = useState<ErrorKey>();
  const queryClient = useQueryClient();
  useEffect(() => {
    if (
      attachment.availability === "ready" ||
      attachment.availability === "unavailable"
    ) {
      queryClient.setQueryData(keys.attachment(attachment.id), attachment);
    }
  }, [attachment, queryClient]);
  const status = useQuery({
    queryKey: keys.attachment(attachment.id),
    queryFn: ({ signal }) => api.attachment(attachment.id, signal),
    retry: false,
    initialData: attachment,
    enabled: visible || (attachment.kind === "audio" && requested),
    staleTime: (query) =>
      query.state.data?.availability === "ready" ||
      query.state.data?.availability === "unavailable"
        ? Infinity
        : 0,
  });
  const current = status.data ?? attachment;
  const failedAttempt = (current.attempt_count ?? 0) > 0;
  const failure = error || (failedAttempt ? "mediaDownload" : undefined);
  const contentURL = `/api/v1/attachments/${encodeURIComponent(attachment.id)}/content`;

  useEffect(() => {
    if (!requested) return;
    const timeout = window.setTimeout(() => {
      setRequested(false);
      setError("mediaTimeout");
    }, 60000);
    return () => window.clearTimeout(timeout);
  }, [requested]);

  useEffect(() => {
    if (
      current.availability === "ready" ||
      current.availability === "unavailable" ||
      status.isError ||
      failedAttempt
    ) {
      setRequested(false);
      if (status.isError) setError(errorKey(status.error));
    }
  }, [
    current.availability,
    status.isError,
    status.error,
    failedAttempt,
    current.attempt_count,
  ]);

  const inlineImage = current.kind === "image" || current.kind === "sticker";
  useEffect(() => {
    if (
      !visible ||
      !status.isFetched ||
      failedAttempt ||
      error ||
      !inlineImage ||
      current.availability !== "remote" ||
      autoRequested.current
    )
      return;
    autoRequested.current = true;
    void download();
  }, [
    visible,
    inlineImage,
    current.availability,
    status.isFetched,
    failedAttempt,
    error,
  ]);

  async function download() {
    setRequested(true);
    setError(undefined);
    try {
      await queryClient.fetchQuery({
        queryKey: [...keys.attachment(attachment.id), "download"],
        queryFn: () =>
          api
            .requestAttachment(attachment.id, AbortSignal.timeout(15000))
            .then(() => true),
        staleTime: 0,
        retry: false,
      });
      const refreshed = await status.refetch();
      if (refreshed.isError) throw refreshed.error;
      if (
        refreshed.data?.availability === "ready" ||
        refreshed.data?.availability === "unavailable"
      )
        setRequested(false);
      return true;
    } catch (cause) {
      setRequested(false);
      setError(errorKey(cause));
      return false;
    }
  }

  return {
    container,
    current,
    activated,
    activate: () => setActivated(true),
    requested,
    failure,
    contentURL,
    download,
  };
}
