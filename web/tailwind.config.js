/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts,js}'],
  theme: {
    extend: {
      colors: {
        // 与 Vue 官方品牌一致的浅色调，避免运维"误以为是 React 项目"。
        brand: {
          DEFAULT: '#42b883',
          dark: '#35495e',
        },
      },
    },
  },
  plugins: [],
}
