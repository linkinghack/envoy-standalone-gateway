import * as DialogPrimitive from "@radix-ui/react-dialog";
import {X} from "lucide-react";
import type {ReactNode} from "react";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({title, description, children}: {title: string; description?: string; children: ReactNode}) {
  return <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-ink/65 backdrop-blur-[2px] data-[state=open]:animate-signal-in" />
    <DialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(92vw,680px)] -translate-x-1/2 -translate-y-1/2 overflow-auto border border-ink bg-paper p-6 shadow-lift">
      <div className="mb-6 pr-8">
        <DialogPrimitive.Title className="font-display text-3xl font-semibold">{title}</DialogPrimitive.Title>
        {description ? <DialogPrimitive.Description className="mt-2 text-sm text-muted">{description}</DialogPrimitive.Description> : null}
      </div>
      {children}
      <DialogPrimitive.Close className="absolute right-4 top-4 grid h-9 w-9 place-items-center border border-line hover:border-ink" aria-label="Close dialog"><X size={17} /></DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>;
}
