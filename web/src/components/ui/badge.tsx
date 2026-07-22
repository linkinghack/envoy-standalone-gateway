import type {HTMLAttributes} from "react";
import {cn} from "../../lib/utils";

type Tone = "healthy" | "warning" | "neutral" | "danger";

export function Badge({tone = "neutral", className, children, ...props}: HTMLAttributes<HTMLSpanElement> & {tone?: Tone}) {
  const tones: Record<Tone, string> = {
    healthy: "border-healthy/40 bg-healthy/10 text-healthy",
    warning: "border-signal/45 bg-signal/10 text-[#a63816]",
    neutral: "border-line bg-ink/[.035] text-muted",
    danger: "border-red-700/40 bg-red-700/10 text-red-800",
  };
  return <span className={cn("inline-flex items-center gap-1.5 border px-2 py-1 font-mono text-[.66rem] font-medium uppercase tracking-wider", tones[tone], className)} {...props}>{children}</span>;
}
