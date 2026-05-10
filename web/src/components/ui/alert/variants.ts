// Alert cva 变体定义（独立模块，避免组件 ↔ barrel 自循环）。详见
// button/variants.ts 文件头注释中"拆出原因"部分。
//
// Alert 用于"页面级提示横幅"：保存成功 / 加载失败 / 操作警告等。语义对应
// 项目原 .alert-* 类（style.css），但走 shadcn-vue 风的 Alert + AlertTitle +
// AlertDescription 三段组合。
//
// role="alert" / role="status" 区别：
//   - 失败/错误：role=alert，aria-live=assertive，屏幕阅读器立刻打断当前
//     朗读、播报新内容
//   - 成功/信息：role=status，aria-live=polite，等当前朗读结束再播报
//
// 这两个 role 不在 cva 变体里管理——而是让消费者在外层显式写
// role="alert" 或 role="status"。否则 cva 把 role 暗藏在 class 字符串里
// 反而违反"语义优于样式"的可访问性原则（变体决定颜色，role 决定屏幕
// 阅读器行为，二者维度不同应该解耦）。
//
// 布局 grid：has-[>svg] 条件 grid（CSS :has() 选择器）+ 子级 col-start-2 协同
//
//   - 默认：grid-cols-[0_1fr]（col 1 宽 0，col 2 占满 1fr）+ 无 has-[>svg]:gap-x-3，
//     AlertTitle / AlertDescription 通过自身 col-start-2 跳到第 2 列 1fr 上，
//     col 1 + gap 都是 0 px，视觉无缩进
//   - 当首层有 svg 子节点：grid-cols-[auto_1fr] + has-[>svg]:gap-x-3，
//     svg 自动落第 1 列、AlertTitle / Description 仍 col-start-2 落第 2 列，
//     列间 12px gap 给图标与文字留呼吸感
//
//   父级条件 grid + 子级 col-start-2 缺一不可：单靠父级 grid 无法控制非 svg
//   子节点落点（CSS auto-placement 会按 row-major 顺序填充导致错位），
//   单靠子级 col-start-2 又会让无 svg 时仍预留 12px gap 形成缩进。
//
//   这样 Alert 既支持"图标 + 文字"也支持"纯文字"，无需改外层结构。
//   :has() 已被所有主流浏览器支持（Chrome 105+ / Firefox 121+ / Safari 15.4+），
//   2024 年起对运维后台用户群可用度极高。
//
// 颜色变体：
//   - default：    border + bg-background，中性提示
//   - destructive：destructive 色系，错误 / 失败
//   - success：    brand 色系，成功 / 完成
//   - warning：    amber 色系，注意 / 警告
//   - info：       info 色系，信息提示
import { cva, type VariantProps } from 'class-variance-authority'

export const alertVariants = cva(
  [
    'relative w-full rounded-lg border px-4 py-3 text-sm',
    'grid grid-cols-[0_1fr] has-[>svg]:grid-cols-[auto_1fr] has-[>svg]:gap-x-3',
    'items-start gap-y-0.5',
    '[&>svg]:size-5 [&>svg]:translate-y-0.5',
  ].join(' '),
  {
    variants: {
      variant: {
        default:     'border-border bg-background text-foreground',
        destructive: 'border-destructive/30 bg-destructive/5 text-destructive [&>svg]:text-destructive dark:border-destructive/50 dark:bg-destructive/10',
        success:     'border-brand-200 bg-brand-50 text-brand-800 [&>svg]:text-brand-600 dark:border-brand-800 dark:bg-brand-900/30 dark:text-brand-200 dark:[&>svg]:text-brand-400',
        warning:     'border-amber-200 bg-amber-50 text-amber-800 [&>svg]:text-amber-600 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-200 dark:[&>svg]:text-amber-400',
        info:        'border-info-200 bg-info-50 text-info-800 [&>svg]:text-info-600 dark:border-info-800 dark:bg-info-900/30 dark:text-info-200 dark:[&>svg]:text-info-400',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

export type AlertVariants = VariantProps<typeof alertVariants>
