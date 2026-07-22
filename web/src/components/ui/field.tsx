import {forwardRef, type InputHTMLAttributes, type TextareaHTMLAttributes} from "react";
import {cn} from "../../lib/utils";

export function Field({label, hint, children}: {label: string; hint?: string; children: React.ReactNode}) {
  return <label className="grid gap-2 text-sm font-medium text-ink">
    <span>{label}</span>
    {children}
    {hint ? <span className="text-xs font-normal text-muted">{hint}</span> : null}
  </label>;
}

const control = "min-h-11 w-full border border-line bg-white/65 px-3 text-sm text-ink transition focus:border-ink focus:bg-white disabled:opacity-50";

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({className, ...props}, ref) => (
  <input ref={ref} className={cn(control, className)} {...props} />
));
Input.displayName = "Input";

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(({className, ...props}, ref) => (
  <textarea ref={ref} className={cn(control, "min-h-28 resize-y py-3 font-mono", className)} {...props} />
));
Textarea.displayName = "Textarea";
