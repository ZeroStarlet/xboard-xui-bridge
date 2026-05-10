// Tooltip 模块 barrel——悬停提示气泡组件家族。
//
// 用法：
//
//   <Tooltip>
//     <TooltipTrigger as-child>
//       <Button variant="ghost" size="icon">
//         <SomeIcon />
//       </Button>
//     </TooltipTrigger>
//     <TooltipContent>{{ t('common.refresh') }}</TooltipContent>
//   </Tooltip>
//
// **必须在 App.vue 根级挂一次 <TooltipProvider />**（批次 7 完成）：Tooltip
// 不再内嵌 Provider，让全局 Provider 的 delayDuration / skipDelayDuration
// 等配置能下传到所有 Tooltip 实例；否则消费者会看到 reka 报错"Tooltip must
// be used within TooltipProvider"。详见 Tooltip.vue 文件头注释。
export { default as Tooltip } from './Tooltip.vue'
export { default as TooltipProvider } from './TooltipProvider.vue'
export { default as TooltipTrigger } from './TooltipTrigger.vue'
export { default as TooltipContent } from './TooltipContent.vue'
