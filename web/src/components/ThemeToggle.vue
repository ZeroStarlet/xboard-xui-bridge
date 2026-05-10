<script setup lang="ts">
// 主题切换器——亮 / 深 / 跟随系统三态。
//
// 视觉策略：
//
//   - 触发器是 Button variant="ghost" + size="icon"（圆形小图标按钮）
//   - 图标根据 isDark 在 Sun（亮）↔ Moon（深）之间切换；用 transition + scale
//     做 reka-ui 风格的"图标渐入渐出"——不能直接换 src 否则会闪烁
//   - 下拉菜单显示 3 个选项 Light / Dark / System，每项前有 Check 标记当前选中
//
// store 协作：useThemeStore 提供 mode（用户选择）+ isDark（实际生效，含
// system 联动）+ setMode；本组件只读 mode 用于"标记选中项"，setMode 触发切换。
//
// i18n：所有菜单项与 aria-label 走 t()，深色模式同样切换语言。
import { useI18n } from 'vue-i18n'
import { Sun, Moon, Monitor, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useThemeStore, type ThemeMode } from '@/stores/theme'

const { t } = useI18n()
const themeStore = useThemeStore()

const items: ReadonlyArray<{ value: ThemeMode; labelKey: 'theme.light' | 'theme.dark' | 'theme.system' }> = [
  { value: 'light',  labelKey: 'theme.light' },
  { value: 'dark',   labelKey: 'theme.dark' },
  { value: 'system', labelKey: 'theme.system' },
]
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <!--
        Sun / Moon 双图标叠加 + transition：
          亮模式（isDark=false）：Sun 显示，Moon 缩到 0
          深模式（isDark=true）： Sun 缩到 0，Moon 显示

        Button 加 `relative` 类给 Moon 的 absolute 定位提供锚点——v0.6 初版
        漏掉这一步，Moon 会按 body 定位而不是 Button，深色态时图标飞到页
        面左上角（批次 6 Codex 第 1 轮指出）。Button 默认 size="icon" 是
        size-9 + flex 居中，加 relative 不影响视觉布局，仅引入定位上下文。

        scale 切换而非 v-if：保持两图标始终在 DOM 内，仅改 scale + rotate，
        让 transition 连续；v-if 移除节点会让动画无源播放。
      -->
      <Button variant="ghost" size="icon" :aria-label="t('theme.toggleAria')" class="relative">
        <Sun
          class="size-[1.2rem] rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0"
        />
        <Moon
          class="absolute size-[1.2rem] rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100"
        />
        <span class="sr-only-soft">{{ t('theme.toggleAria') }}</span>
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        v-for="item in items"
        :key="item.value"
        @select="themeStore.setMode(item.value)"
      >
        <!-- Sun/Moon/Monitor icon 视图——前置图标 + 选中态 √ 后置 -->
        <Sun     v-if="item.value === 'light'"  class="size-4" />
        <Moon    v-else-if="item.value === 'dark'" class="size-4" />
        <Monitor v-else class="size-4" />
        <span class="flex-1">{{ t(item.labelKey) }}</span>
        <Check v-if="themeStore.mode === item.value" class="size-4 opacity-100" />
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
