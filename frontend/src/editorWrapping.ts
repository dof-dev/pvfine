export const longLineThreshold = 10_000;

// Scan without splitting the document or allocating copies of long lines.
export function hasLongLine(text: string): boolean {
  let length = 0;
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index);
    if (code === 10 || code === 13) {
      length = 0;
    } else if (++length > longLineThreshold) {
      return true;
    }
  }
  return false;
}
