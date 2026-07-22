import {cva, type VariantProps} from "class-variance-authority";
import {forwardRef, type ButtonHTMLAttributes} from "react";
import {cn} from "../../lib/utils";

const buttonVariants = cva(
  "inline-flex min-h-10 items-center justify-center gap-2 border px-4 text-sm font-semibold transition-[background,color,border-color,transform,box-shadow] disabled:pointer-events-none disabled:opacity-45 active:translate-y-px",
  {
    variants: {
      variant: {
        primary: "border-signal bg-signal text-white shadow-[3px_3px_0_rgb(var(--ink))] hover:bg-ink hover:border-ink",
        secondary: "border-ink bg-paper text-ink hover:bg-ink hover:text-paper",
        ghost: "border-transparent bg-transparent text-current hover:border-line hover:bg-white/10",
        danger: "border-red-700 bg-red-700 text-white hover:bg-red-800",
      },
      size: {
        sm: "min-h-8 px-3 text-xs",
        md: "min-h-10 px-4",
        lg: "min-h-12 px-6 text-base",
        icon: "h-10 w-10 p-0",
      },
    },
    defaultVariants: {variant: "primary", size: "md"},
  },
);

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(({className, variant, size, ...props}, ref) => (
  <button ref={ref} className={cn(buttonVariants({variant, size}), className)} {...props} />
));
Button.displayName = "Button";
