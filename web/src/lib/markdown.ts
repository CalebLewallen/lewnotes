// Search snippets already contain <mark>; allow only those spans.
import DOMPurify from 'dompurify';

export function sanitizeSnippet(snippet: string): string {
  return DOMPurify.sanitize(snippet, { ALLOWED_TAGS: ['mark'], ALLOWED_ATTR: [] });
}
