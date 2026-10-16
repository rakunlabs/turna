// https://vitepress.dev/guide/custom-theme
import DefaultTheme from 'vitepress/theme'
import './style.css'

export default {
  extends: DefaultTheme,
  enhanceApp() {
    if (typeof window === 'undefined') return

    // Show the sidebar scrollbar only while the sidebar is being scrolled.
    const timers = new WeakMap<Element, number>()
    document.addEventListener(
      'scroll',
      (event) => {
        const el = event.target
        if (!(el instanceof Element) || !el.classList.contains('VPSidebar')) return

        el.classList.add('is-scrolling')
        window.clearTimeout(timers.get(el))
        timers.set(el, window.setTimeout(() => el.classList.remove('is-scrolling'), 800))
      },
      { capture: true, passive: true },
    )
  },
}
