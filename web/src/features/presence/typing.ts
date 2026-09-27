export function createTypingSender(
  send: (activity: "typing" | "paused") => Promise<void>,
) {
  let active = false;
  let lastSent = -Infinity;
  let idle: ReturnType<typeof setTimeout> | undefined;
  let pending: "typing" | "paused" | undefined;
  let sending = false;
  let closed = false;
  async function flush() {
    if (sending) return;
    sending = true;
    try {
      while (pending) {
        const activity = pending;
        pending = undefined;
        try {
          await send(activity);
        } catch {
          /* Presence is best effort; the next input can refresh it. */
        }
      }
    } finally {
      sending = false;
    }
  }
  function stop() {
    clearTimeout(idle);
    if (!active) return;
    active = false;
    pending = "paused";
    void flush();
  }
  return {
    input(hasText: boolean) {
      if (closed) return;
      if (!hasText) {
        stop();
        return;
      }
      clearTimeout(idle);
      idle = setTimeout(stop, 4000);
      if (!active || Date.now() - lastSent >= 3000) {
        active = true;
        lastSent = Date.now();
        pending = "typing";
        void flush();
      }
    },
    stop,
    close() {
      closed = true;
      stop();
    },
  };
}
