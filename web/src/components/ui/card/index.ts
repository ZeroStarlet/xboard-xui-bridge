// Card 模块导出。
//
// shadcn-vue 约定：每个组件单独 .vue 文件；index.ts 集中再导出，让消费者
// 一行 import 就能拿到整套：
//
//   import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card'
export { default as Card } from './Card.vue'
export { default as CardHeader } from './CardHeader.vue'
export { default as CardTitle } from './CardTitle.vue'
export { default as CardDescription } from './CardDescription.vue'
export { default as CardContent } from './CardContent.vue'
export { default as CardFooter } from './CardFooter.vue'
