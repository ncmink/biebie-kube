/** Tokens the expression parser accepts, for prefix autocomplete. */
export const querySuggestions = [
  'name',
  'namespace',
  'status',
  'health',
  'restarts',
  'age',
  'cpu',
  'memory',
  'missing(',
  'AND',
] as const

export type QuerySuggestion = (typeof querySuggestions)[number]

const minPrefix = 2

/**
 * suggestQueryTokens returns completions for the unfinished token at the
 * caret. Two characters is enough to distinguish fields without opening
 * the menu on every first letter.
 */
export function suggestQueryTokens(expression: string, caret: number): QuerySuggestion[] {
  const before = expression.slice(0, caret)
  const token = currentToken(before)
  if (token.length < minPrefix) return []

  const needle = token.toLowerCase()
  return querySuggestions.filter((item) => {
    if (item === token) return false
    return item.toLowerCase().startsWith(needle)
  })
}

export function applyQuerySuggestion(
  expression: string,
  caret: number,
  suggestion: string,
): { next: string; caret: number } {
  const before = expression.slice(0, caret)
  const after = expression.slice(caret)
  const token = currentToken(before)
  const start = before.length - token.length
  const next = before.slice(0, start) + suggestion + after
  return { next, caret: start + suggestion.length }
}

function currentToken(before: string): string {
  const match = before.match(/[A-Za-z(]+$/)
  return match?.[0] ?? ''
}
