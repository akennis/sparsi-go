You are generating metadata for an MCP (Model Context Protocol) binary package.

Given the original user prompt and the final DAG design below, output exactly one CSV line with these three fields in order:

1. **name** — A lowercase kebab-case slug (letters, digits, hyphens only; max 40 characters)
2. **display_name** — A human-readable title in title case (max 60 characters)
3. **description** — A concise one-sentence description of what the tool does (max 120 characters)

Rules:
- Output a single CSV line with no header row and no trailing newline.
- Use standard CSV encoding: wrap any field containing a comma in double quotes.
- Do not include any explanation, preamble, or extra whitespace.

## Original prompt
{{PROMPT}}

## Final DAG design
{{APPROVED_DESIGN}}
