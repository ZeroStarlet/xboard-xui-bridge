// Badge cva 变体定义（独立模块，避免组件 ↔ barrel 自循环）。详见
// button/variants.ts 文件头注释中"拆出原因"部分。
//
// Badge 用于状态标签 / pill：例如"启用" / "运行中" / "已停止" / "未配置"。
// 与项目原 .pill-* 类（style.css）功能等价，但用 shadcn-vue 风的 cva
// 变体管理：消费者写 <Badge variant="success">启用</Badge> 而不是
// <span class="pill-success">...</span>。
//
// 颜色变体：
//
//   - default：    bg-primary（emerald-700）+ 白文字，主要状态
//   - secondary：  bg-secondary 灰底 + foreground，"中性"标签
//   - destructive：bg-destructive（rose-600）+ 白文字，危险/失败状态
//   - outline：    透明底 + 边框 + foreground，"低强度提示"
//   - success：    自定义品牌色变体——bg-brand-50/border-brand-200/text-brand-700，
//                  对应原 .pill-success；用于"启用 / 在线 / 健康"
//   - warning：    bg-amber-50/border-amber-200/text-amber-700，对应 .pill-warning
//   - info：       bg-info-50/border-info-200/text-info-700，对应 .pill-info
//
// success/warning/info 是 shadcn-vue 官方 Badge 没有的扩展——保留是
// 因为运维场景对"绿色=好 / 黄色=注意 / 蓝色=信息"色彩编码强依赖，纯
// primary/destructive 二元色覆盖不了细粒度。深色模式下这三个变体也保留
// brand/amber/info 色阶（不跟随 .dark 切换语义 token）——色阶在亮深两边
// 都是高对比度小色块，视觉上仍可读，且色彩编码语义与亮模式一致更利于
// 用户跨主题阅读。
import { cva, type VariantProps } from 'class-variance-authority'

export const badgeVariants = cva(
  [
    'inline-flex items-center justify-center gap-1.5',
    'rounded-md border px-2 py-0.5 text-xs font-medium',
    'transition-colors',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
  ].join(' '),
  {
    variants: {
      variant: {
        default:     'border-transparent bg-primary text-primary-foreground',
        secondary:   'border-transparent bg-secondary text-secondary-foreground',
        destructive: 'border-transparent bg-destructive text-destructive-foreground',
        outline:     'text-foreground',
        success:     'border-brand-200 bg-brand-50 text-brand-700 dark:border-brand-800 dark:bg-brand-900/40 dark:text-brand-300',
        warning:     'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
        info:        'border-info-200 bg-info-50 text-info-700 dark:border-info-800 dark:bg-info-900/40 dark:text-info-300',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

export type BadgeVariants = VariantProps<typeof badgeVariants>
