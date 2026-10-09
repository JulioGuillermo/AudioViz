# Agents (MCP server)

The studio is also an **MCP server** (Model Context Protocol): an AI agent — Claude Code, Claude
Desktop or any MCP client — can open projects, change them, look at frames, read the song's
analysis, generate images and lyrics, and export. Whatever it changes is saved and shows in the
open studio at once, as a step that **Ctrl+Z** undoes.

It runs on the studio's own address:

| Transport | URL |
|---|---|
| Streamable HTTP | `http://<host>:<port>/mcp` |
| SSE (older clients) | `http://<host>:<port>/sse` |

```bash
# on the machine with the GPU, open to the network, with a token
audioviz studio --addr 0.0.0.0:8787 --no-open --mcp-token MY-TOKEN
# (or AUDIOVIZ_MCP_TOKEN=MY-TOKEN)
```

With a token, every request must carry it: `Authorization: Bearer MY-TOKEN` (or `?token=MY-TOKEN`
in the URL, for clients that can't send headers). Without one, only requests that don't come from
another web page are taken — fine on your own machine (`127.0.0.1`). The token protects the MCP
endpoints; the studio's web page itself stays open to whoever reaches the address, so keep it on a
network you trust.

## Connecting a client

Claude Code:

```bash
claude mcp add --transport http audioviz http://192.168.1.50:8787/mcp --header "Authorization: Bearer MY-TOKEN"
# or the SSE transport
claude mcp add --transport sse audioviz http://192.168.1.50:8787/sse --header "Authorization: Bearer MY-TOKEN"
```

Clients configured with JSON:

```json
{ "mcpServers": { "audioviz": { "type": "http", "url": "http://192.168.1.50:8787/mcp",
    "headers": { "Authorization": "Bearer MY-TOKEN" } } } }
```

## What it can do

| Tool | |
|---|---|
| `studio_state`, `list_projects`, `open_project`, `new_project` | what is open; the projects; open or create one |
| `get_project`, `edit_project` | read the project (JSON) and change it: JSON Pointer operations (`set`, `merge`, `insert`, `remove`), all or nothing |
| `render_frame` | a frame as an image (the agent sees what it did) |
| `audio_analysis` | bands, beats and — with the song separated — kick, snare and hats hits over a stretch of time |
| `try_expression` | check an [expression](expressions.md) on a property: its values or its mistake |
| `get_docs` | this documentation (also as MCP resources) |
| `list_files`, `upload_file` | files on the studio's machine; send one into the project |
| `get_lyrics`, `set_lyrics`, `generate_lyrics` | the synced lyrics; sync them from a text or the voice |
| `generate_image`, `list_images` | images with the local AI |
| `separate_stems`, `compute_depth` | separate the song; the depth of a background |
| `export_video`, `job_status`, `cancel_job` | render the video; follow the long jobs |

E.g. *"put the title 'NIGHT DRIVE' from 0 to 8 s, glowing with the kick, letters in a rainbow that
moves"* becomes an `edit_project` that inserts a text with `exprs: {glow.intensity: "0.4 + kick",
color: "hsv(text_x * 300 + time * 60, 0.9, 1)"}`, checked with `render_frame`.
