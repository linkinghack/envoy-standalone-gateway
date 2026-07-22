import type {ReactNode} from "react";

export function PageHeading({eyebrow, title, description, action}: {eyebrow: string; title: string; description: string; action?: ReactNode}) {
  return <header className="mb-8 grid gap-5 border-b border-ink/20 pb-7 md:grid-cols-[1fr_auto] md:items-end">
    <div className="max-w-3xl animate-signal-in">
      <p className="eyebrow mb-3 text-signal">{eyebrow}</p>
      <h1 className="font-display text-4xl font-semibold leading-[.95] tracking-[-.025em] md:text-6xl">{title}</h1>
      <p className="mt-4 max-w-2xl text-sm leading-6 text-muted md:text-base">{description}</p>
    </div>
    {action ? <div className="animate-signal-in [animation-delay:100ms]">{action}</div> : null}
  </header>;
}
