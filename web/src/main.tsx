import "./i18n";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { motionTiming } from "./ui/motion";
import "@fontsource-variable/inter";
import { App } from "./app/App";
import "./styles/global.css";

try {
  document.documentElement.dataset.theme =
    localStorage.getItem("convomeow-theme") === "dark" ? "dark" : "light";
} catch {
  document.documentElement.dataset.theme = "light";
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  },
});

const root = document.getElementById("root");
if (!root) throw new Error("Root element is missing");

createRoot(root).render(
  <QueryClientProvider client={queryClient}>
    <BrowserRouter basename="/app">
      <MotionConfig reducedMotion="user" transition={motionTiming.enter}>
        <App />
      </MotionConfig>
    </BrowserRouter>
  </QueryClientProvider>,
);
