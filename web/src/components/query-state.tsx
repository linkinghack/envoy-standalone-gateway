import {AlertTriangle, Inbox} from "lucide-react";
import {Button} from "./ui/button";

export function LoadingBlock({label = "Reading gateway state"}: {label?: string}) {
  return <div className="grid min-h-48 place-items-center border border-line bg-paper/60"><div className="text-center"><div className="mx-auto h-8 w-8 animate-spin border-2 border-line border-t-signal" /><p className="eyebrow mt-4 text-muted">{label}</p></div></div>;
}

export function ErrorBlock({message, retry}: {message: string; retry?: () => void}) {
  return <div role="alert" className="border border-red-800/30 bg-red-50 p-6 text-red-950"><AlertTriangle /><h2 className="mt-3 font-display text-2xl">The signal broke</h2><p className="mt-2 text-sm">{message}</p>{retry ? <Button className="mt-5" variant="secondary" onClick={retry}>Try again</Button> : null}</div>;
}

export function EmptyBlock({title, description}: {title: string; description: string}) {
  return <div className="grid min-h-48 place-items-center border border-dashed border-line bg-paper/45 p-8 text-center"><div><Inbox className="mx-auto text-muted" /><h2 className="mt-3 font-display text-2xl">{title}</h2><p className="mt-2 max-w-sm text-sm text-muted">{description}</p></div></div>;
}
