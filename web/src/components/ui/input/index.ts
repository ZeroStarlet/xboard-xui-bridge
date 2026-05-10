// Input 模块导出。
//
// 仅导出 Input 组件本体——shadcn-vue 的 Input 没有 cva 变体，所以
// 不像 Button 那样需要导出 variants 函数。
export { default as Input } from './Input.vue'
