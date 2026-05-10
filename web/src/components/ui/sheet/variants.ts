// Sheet（侧滑抽屉）cva 变体——side 维度提供 top/right/bottom/left 四向滑出。
//
// 与 Dialog（中央模态）的关系：两者都用 reka-ui Dialog primitives，但
// Sheet 强调"非模态阻断"的工作流（用户能边看背景列表边填表单），且滑入
// 动画方向与"任务单流入"语义一致；Dialog 是中心居中 + zoom 动画，强调
// "阻断决策"。批次 9 的 Bridges 表单用 Sheet（保留 v0.5 抽屉体验），
// 批次 9 的删除确认用 Dialog（强阻断防误删）。
//
// 动画依赖 tailwindcss-animate plugin（已在 batch 1/2 安装并注册）：
//   - data-[state=open]:animate-in / data-[state=closed]:animate-out
//   - slide-in-from-right / slide-out-to-right 等方向 utility
//
// 尺寸：
//   - top/bottom：默认占满宽度，h-auto（内容决定高度），上下方向滑入
//   - left/right：默认 w-3/4 sm:max-w-sm，占屏幕右/左侧三分之一到全宽（小屏全宽，
//     大屏限制 max-w-sm = 24rem）；高度 inset-y-0 占满整高，更像桌面端抽屉
import { cva, type VariantProps } from 'class-variance-authority'

export const sheetVariants = cva(
  [
    'fixed z-50 gap-4 bg-background p-6 shadow-lg',
    'transition ease-in-out',
    'data-[state=open]:animate-in data-[state=closed]:animate-out',
    'data-[state=closed]:duration-300 data-[state=open]:duration-500',
  ].join(' '),
  {
    variants: {
      side: {
        top:    'inset-x-0 top-0 border-b data-[state=closed]:slide-out-to-top data-[state=open]:slide-in-from-top',
        bottom: 'inset-x-0 bottom-0 border-t data-[state=closed]:slide-out-to-bottom data-[state=open]:slide-in-from-bottom',
        left:   'inset-y-0 left-0 h-full w-3/4 border-r data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left sm:max-w-sm',
        // right：本项目 Bridges 表单沿用 v0.5 的"右侧抽屉"——把 max-w 调到 lg
        // (32rem)，比 shadcn-vue 默认 sm 略宽，让 grid-cols-2 的字段对适配更舒展
        right:  'inset-y-0 right-0 h-full w-3/4 border-l data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right sm:max-w-lg',
      },
    },
    defaultVariants: {
      side: 'right',
    },
  },
)

export type SheetVariants = VariantProps<typeof sheetVariants>
