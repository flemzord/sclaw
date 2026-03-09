---
name: telegraph
description: "Publish articles to Telegra.ph (Telegraph) using the createPage API via curl. Use when: user asks to publish, post, or create a page on Telegraph/Telegra.ph, or when a cron/task needs to publish formatted content to a Telegraph page. Also triggers for article publishing, blog posting to Telegraph, or generating readable web pages from content."
trigger: auto
keywords:
  - telegraph
  - telegra.ph
  - publish
  - article
metadata: { "openclaw": { "emoji": "📝", "requires": { "bins": ["curl"] } } }
---

# Telegraph Publishing Skill

Publish formatted articles to [Telegra.ph](https://telegra.ph) using the API.

## When to Use

✅ **USE this skill when:**

- Publishing content to Telegra.ph (news reviews, summaries, reports)
- Creating a readable web page from compiled content
- A cron job needs to publish and share a link
- "Publie ça sur Telegraph" / "Create a Telegraph page"

❌ **DON'T use this skill when:**

- Reading or scraping existing Telegraph pages (just use `curl` directly)
- Managing a Telegraph account (editing, revoking tokens)

## Authentication

The Telegraph access token is a plain-text file. Read it with `read_file` before calling the API.

Common token locations (check agent workspace or secrets directory):
- `secrets/telegraph.txt`
- `workspace/telegraph-token.txt`

## Publishing a Page

### API Call

```bash
curl -s -X POST "https://api.telegra.ph/createPage" \
  -H "Content-Type: application/json" \
  -d '{
    "access_token": "TOKEN",
    "title": "Your Page Title",
    "author_name": "Sclaw",
    "content": CONTENT_JSON,
    "return_content": false
  }'
```

### Response

```json
{
  "ok": true,
  "result": {
    "path": "Your-Page-Title-03-09",
    "url": "https://telegra.ph/Your-Page-Title-03-09",
    "title": "Your Page Title",
    "author_name": "Sclaw",
    "views": 0
  }
}
```

The published URL is in `result.url`.

## Content Format

The `content` field is a JSON array of node objects. Each node has a `tag` and `children`.

### Available Tags

| Tag | Usage |
|-----|-------|
| `h3` | Section heading |
| `h4` | Sub-heading |
| `p` | Paragraph |
| `strong` | Bold text |
| `em` | Italic text |
| `a` | Link (`href` attribute) |
| `blockquote` | Quote block |
| `ul`, `li` | Bullet list |
| `br` | Line break |
| `figure`, `img` | Image |

### Node Structure

```json
{"tag": "p", "children": ["Plain text content"]}
```

Nested elements:

```json
{"tag": "p", "children": [{"tag": "strong", "children": ["Bold"]}, " then normal"]}
```

Links:

```json
{"tag": "p", "children": [{"tag": "a", "attrs": {"href": "https://example.com"}, "children": ["Click here"]}]}
```

### Example: Article with Sections

```json
[
  {"tag": "p", "children": ["Introduction paragraph."]},
  {"tag": "h3", "children": ["🌍 Section Title"]},
  {"tag": "p", "children": ["Section content with details."]},
  {"tag": "h3", "children": ["💻 Another Section"]},
  {"tag": "p", "children": ["More content here."]},
  {"tag": "p", "children": [{"tag": "em", "children": ["Footer — Source attribution — Date"]}]}
]
```

## Critical Rules for JSON Content

Telegraph content is sent as JSON inside a JSON body. This creates escaping challenges:

1. **No curly/typographic quotes** (`'` `'` `"` `"`) — use straight ASCII quotes only
2. **No unescaped apostrophes in French text** — replace `l'article` with `l article` or rephrase (`la suite de l article` → `cet article`)
3. **Escape inner double quotes** — use `\"` inside JSON strings
4. **Write the JSON to a temp file** if the content is long, then use `curl -d @/tmp/telegraph.json` to avoid shell escaping issues

### Recommended Pattern for Long Content

Instead of fighting shell escaping, write the request body to a file:

```bash
# 1. Build the JSON body and write to file (via write_file tool)
# 2. POST from file
curl -s -X POST "https://api.telegra.ph/createPage" \
  -H "Content-Type: application/json" \
  -d @/tmp/telegraph-request.json
```

This avoids all shell escaping problems.

## Complete Workflow Example

Here's the typical flow for publishing a compiled article:

1. **Gather content** — fetch sources, compile topics
2. **Read token** — `read_file secrets/telegraph.txt`
3. **Build request body** — construct the JSON with title, author, and content nodes
4. **Write to temp file** — `write_file /tmp/telegraph-request.json` with the full request body
5. **Publish** — `exec curl -s -X POST "https://api.telegra.ph/createPage" -H "Content-Type: application/json" -d @/tmp/telegraph-request.json`
6. **Extract URL** — parse `result.url` from the response
7. **Share** — include the URL in the final message or notification
