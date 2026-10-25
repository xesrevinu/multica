export type MarkdownBlock =
  | { kind: "heading"; level: number; text: string }
  | { kind: "paragraph"; text: string }
  | { kind: "code"; lang?: string; lines: string[] }
  | { kind: "list"; ordered: boolean; items: string[] }

function preprocessMentions(source: string) {
  return source
    .replace(/\[@([^\]]+)\]\(mention:\/\/[^/]+\/([^)]+)\)/g, "@$1")
    .replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, "$1 ($2)")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
}

export function parseMarkdown(source: string): MarkdownBlock[] {
  const text = preprocessMentions(source).replace(/\r\n/g, "\n")
  const lines = text.split("\n")
  const blocks: MarkdownBlock[] = []
  let index = 0

  while (index < lines.length) {
    const line = lines[index]
    if (!line.trim()) {
      index++
      continue
    }

    const fence = line.match(/^(`{3,}|~{3,})(\w*)$/)
    if (fence) {
      const marker = fence[1]
      const lang = fence[2] || undefined
      const code: string[] = []
      index++
      while (index < lines.length && !lines[index].startsWith(marker)) {
        code.push(lines[index])
        index++
      }
      if (index < lines.length) index++
      blocks.push({ kind: "code", lang, lines: code })
      continue
    }

    const heading = line.match(/^(#{1,6})\s+(.*)$/)
    if (heading) {
      blocks.push({ kind: "heading", level: heading[1].length, text: heading[2].trim() })
      index++
      continue
    }

    const bullet = line.match(/^(\s*)([-*+]|\d+\.)\s+(.*)$/)
    if (bullet) {
      const ordered = /\d+\./.test(bullet[2])
      const items: string[] = []
      while (index < lines.length) {
        const current = lines[index]
        const match = current.match(/^(\s*)([-*+]|\d+\.)\s+(.*)$/)
        if (!match) break
        items.push(match[3].trim())
        index++
      }
      blocks.push({ kind: "list", ordered, items })
      continue
    }

    const paragraph: string[] = [line.trim()]
    index++
    while (index < lines.length && lines[index].trim() && !lines[index].match(/^(#{1,6})\s/)) {
      const next = lines[index]
      if (next.match(/^(`{3,}|~{3,})/)) break
      if (next.match(/^(\s*)([-*+]|\d+\.)\s+/)) break
      paragraph.push(next.trim())
      index++
    }
    blocks.push({ kind: "paragraph", text: paragraph.join(" ") })
  }

  return blocks
}
