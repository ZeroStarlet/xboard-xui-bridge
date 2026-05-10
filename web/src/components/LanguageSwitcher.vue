<script setup lang="ts">
// 语言切换器——zh-CN / en-US 二态。
//
// 视觉策略：
//
//   - 触发器是 Button variant="ghost" + size="icon"，与 ThemeToggle 视觉对齐
//   - 图标 Languages（lucide），有"语言"语义即可
//   - 下拉菜单显示当前支持的所有 locale，每项以本地原文显示（"简体中文" /
//     "English"）—— 用户切换前先看到"目标语言长什么样"，零误操作
//   - 当前生效项前有 Check 标记
//
// store 协作：useLocaleStore 提供 current + setLocale；setLocale 内部已处理
// i18n.global.locale.value 同步 + localStorage 持久化 + html lang 属性。
import { useI18n } from 'vue-i18n'
import { Languages, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useLocaleStore } from '@/stores/locale'
import { SUPPORTED_LOCALES, type SupportedLocale } from '@/i18n'

const { t } = useI18n()
const localeStore = useLocaleStore()

// locale.zh-CN / locale.en-US 都是 i18n 中的可朗读名（zh 内是"简体中文"、
// en 内是"English"）；显示在菜单项上时用对应 locale 的 message 让用户看到
// "目标语言的本地原文"。
function localeLabel(loc: SupportedLocale): string {
  return t(`locale.${loc}`)
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="ghost" size="icon" :aria-label="t('locale.toggleAria')">
        <Languages class="size-[1.2rem]" />
        <span class="sr-only-soft">{{ t('locale.toggleAria') }}</span>
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        v-for="loc in SUPPORTED_LOCALES"
        :key="loc"
        @select="localeStore.setLocale(loc)"
      >
        <span class="flex-1">{{ localeLabel(loc) }}</span>
        <Check v-if="localeStore.current === loc" class="size-4" />
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
