/** One row in a right-click menu. */
export interface ContextMenuItem {
  id: string
  label: string
  disabled?: boolean
  danger?: boolean
  /** Draws a separator above this row. */
  divider?: boolean
}
