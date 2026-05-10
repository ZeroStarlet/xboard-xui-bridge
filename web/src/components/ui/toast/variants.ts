// Toast cva 变体——颜色按 variant 区分（与 Alert 同色系），结构与单条 toast 行内布局。
//
// data-state / data-swipe 属性由 reka-ui ToastRoot 自动设置，让 Tailwind 可
// 用 data-[state=...]:slide-in-from-right 等 utility 做出场/退场动画。
import { cva, type VariantProps } from 'class-variance-authority'

export const toastVariants = cva(
  [
    'group pointer-events-auto relative flex w-full items-center justify-between space-x-2 overflow-hidden',
    'rounded-md border p-4 pr-6 shadow-lg transition-all',
    'data-[swipe=cancel]:translate-x-0',
    'data-[swipe=end]:translate-x-[var(--reka-toast-swipe-end-x)]',
    'data-[swipe=move]:translate-x-[var(--reka-toast-swipe-move-x)] data-[swipe=move]:transition-none',
    'data-[state=open]:animate-in data-[state=closed]:animate-out data-[swipe=end]:animate-out',
    'data-[state=closed]:fade-out-80 data-[state=closed]:slide-out-to-right-full',
    'data-[state=open]:slide-in-from-top-full data-[state=open]:sm:slide-in-from-bottom-full',
  ].join(' '),
  {
    variants: {
      variant: {
        default:     'border bg-background text-foreground',
        destructive: 'destructive group border-destructive bg-destructive text-destructive-foreground',
        success:     'border-brand-200 bg-brand-50 text-brand-900 dark:border-brand-800 dark:bg-brand-950 dark:text-brand-100',
        warning:     'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100',
        info:        'border-info-200 bg-info-50 text-info-900 dark:border-info-800 dark:bg-info-950 dark:text-info-100',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)

export type ToastVariants = VariantProps<typeof toastVariants>
