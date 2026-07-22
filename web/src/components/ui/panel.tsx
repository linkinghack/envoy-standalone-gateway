import type {HTMLAttributes} from "react";
import {cn} from "../../lib/utils";

export function Panel({className, ...props}: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("border border-line bg-paper shadow-panel", className)} {...props} />;
}

export function PanelHeader({className, ...props}: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-start justify-between gap-4 border-b border-line px-5 py-4", className)} {...props} />;
}
