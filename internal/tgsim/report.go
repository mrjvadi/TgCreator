package tgsim

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Transcript renders the conversation as Markdown, chronologically.
func (s *Sim) Transcript() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, e := range s.Events {
		switch e.Kind {
		case "note":
			fmt.Fprintf(&b, "\n%s\n\n", e.Text)
		case "filtered":
			fmt.Fprintf(&b, "- ⚪️ _%s_\n", e.Text)
		case "error":
			fmt.Fprintf(&b, "- ❗️ `%s`\n", e.Text)
		default:
			fmt.Fprintf(&b, "- **[%s]** %s: %s\n", e.Chat, e.Who, strings.ReplaceAll(e.Text, "\n", " ⏎ "))
		}
	}
	return b.String()
}

// HTMLTranscript renders the conversation as chat bubbles, one column per
// chat, for humans to review.
func (s *Sim) HTMLTranscript(title string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + `</title><style>
:root{--bg:#f4f4f5;--card:#fff;--ink:#18181b;--muted:#71717a;--user:#e4e4e7;--bot:#dbeafe;--act:#fef3c7;--err:#fee2e2;--line:#e4e4e7}
@media (prefers-color-scheme:dark){:root{--bg:#111113;--card:#1c1c1f;--ink:#f4f4f5;--muted:#a1a1aa;--user:#2a2a2e;--bot:#1e3a5f;--act:#3f3212;--err:#4c1d1d;--line:#2a2a2e}}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.6 system-ui,"Vazirmatn",Tahoma,sans-serif}
main{max-width:860px;margin:0 auto;padding:16px}
h1{font-size:22px;margin:8px 0 16px}
h2{font-size:16px;margin:28px 0 8px;padding-top:12px;border-top:1px solid var(--line)}
.ev{display:flex;gap:8px;margin:6px 0;align-items:flex-start}
.chat{flex:0 0 auto;font-size:12px;color:var(--muted);min-width:110px;padding-top:6px}
.bub{padding:6px 10px;border-radius:10px;white-space:pre-wrap;word-break:break-word;max-width:100%}
.user .bub{background:var(--user)}.bot .bub{background:var(--bot)}.action .bub{background:var(--act)}
.error .bub,.blocked .bub{background:var(--err);font-family:ui-monospace,monospace;font-size:13px}
.filtered .bub{background:transparent;color:var(--muted);font-style:italic}
.who{font-weight:600;margin-left:6px}
@media (max-width:560px){.ev{flex-direction:column;gap:2px}.chat{padding:0}}
</style></head><body><main><h1>` + html.EscapeString(title) + `</h1>`)
	for _, e := range s.Events {
		if e.Kind == "note" {
			b.WriteString("<h2>" + html.EscapeString(strings.TrimLeft(e.Text, "# ")) + "</h2>")
			continue
		}
		chat := e.Chat
		if chat == "" {
			if c, ok := s.chats[e.ChatID]; ok {
				chat = c.Title
			}
		}
		who := ""
		if e.Who != "" {
			who = `<span class="who">` + html.EscapeString(e.Who) + `:</span>`
		}
		fmt.Fprintf(&b, `<div class="ev %s"><div class="chat">%s</div><div class="bub">%s%s</div></div>`,
			e.Kind, html.EscapeString(chat), who, html.EscapeString(e.Text))
	}
	b.WriteString("</main></body></html>")
	return b.String()
}

// Summary counts calls per method.
func (s *Sim) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := map[string]int{}
	for _, c := range s.Calls {
		count[c.Method]++
	}
	names := make([]string, 0, len(count))
	for n := range count {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", n, count[n]))
	}
	return strings.Join(parts, ", ")
}
