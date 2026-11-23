import { useTheme } from '../../lib/theme.ts'

export function ThemeToggle() {
  const { theme, toggle } = useTheme()
  const dark = theme === 'dark'
  return (
    <button type="button" className="btn btn-light btn-icon" aria-pressed={dark} title={dark ? 'Ganti ke mode terang' : 'Ganti ke mode gelap'} onClick={toggle}>
      <i className="fas fa-moon theme-icon-dark" aria-hidden="true" />
      <i className="fas fa-sun theme-icon-light" aria-hidden="true" />
      <span className="visually-hidden">Ganti tema gelap atau terang</span>
    </button>
  )
}
