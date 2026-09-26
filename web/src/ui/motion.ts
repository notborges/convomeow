import type { Transition } from "motion/react";

export const motionTiming = {
  feedback: { duration: 0.14, ease: "easeOut" },
  enter: { duration: 0.22, ease: [0.16, 1, 0.3, 1] },
  selection: { type: "spring", stiffness: 500, damping: 38 },
} satisfies Record<string, Transition>;
