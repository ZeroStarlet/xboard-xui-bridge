<script setup lang="ts">
// shadcn-vue Switch（iOS-style 拨动开关），基于 reka-ui SwitchRoot/Thumb。
//
// 用法：
//
//   <Switch v-model="form.alive_enabled" id="alive" />
//   <Label for="alive">在线 IP 上报</Label>
//
// 与 Checkbox 的语义区别：
//
//   - Switch：表达"立即生效的开关"——切换即触发副作用（保存到后端 / 应用配置），
//     视觉强调"通断"。运维场景里"启用上报"这类二元开关用 Switch 更清晰。
//   - Checkbox：表达"勾选 / 同意"——可在表单内多选，提交时才生效，视觉强调
//     "选中态"。
//
// reka-ui SwitchRoot 已处理：
//   - role="switch" + aria-checked 同步
//   - 键盘 Space / Enter 切换
//   - data-[state=checked|unchecked] 属性供 Tailwind 状态选择器
//   - modelValue / update:modelValue —— v-model 透明对接：父级 <Switch v-model="x" />
//     的 modelValue + update:modelValue 直接被 reka SwitchRoot 消费/触发，
//     无需手写 defineModel 或 emit 声明。
//
// 视觉：38x22px 轨道 + 18x18px 圆点，data-[state=checked]:bg-primary 让开
//      启时填充品牌色，未开启时是 muted 灰底。圆点平移由 CSS transition
//      处理（translate-x-0/translate-x-4）。
import { computed, type HTMLAttributes } from 'vue'
import { SwitchRoot, SwitchThumb, type SwitchRootEmits, type SwitchRootProps, useForwardPropsEmits } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends SwitchRootProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()
const emits = defineEmits<SwitchRootEmits>()

const delegatedProps = computed(() => {
  // 仅剥离本组件包装的 class，其余 props 全量透传给 reka SwitchRoot。
  // 不剥 modelValue / defaultValue：v-model 透明转发依赖它们走完整链路。
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <SwitchRoot
    v-bind="forwarded"
    :class="
      cn(
        'peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent',
        'shadow-sm transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'data-[state=checked]:bg-primary data-[state=unchecked]:bg-input',
        props.class,
      )
    "
  >
    <SwitchThumb
      :class="
        cn(
          'pointer-events-none block size-4 rounded-full bg-background shadow-lg ring-0',
          'transition-transform',
          'data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0',
        )
      "
    />
  </SwitchRoot>
</template>
