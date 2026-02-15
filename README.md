# GitPet (gh-pet)

GitPet is a GitHub CLI extension that turns your GitHub activity into a digital companion living in the Cache.

## Install

```bash
gh extension install <owner>/gh-pet
```

## Commands

```bash
gh pet feed    # Sync recent GitHub activity and update pet stats
gh pet status  # Render the current pet state
gh pet suggest # Ask Copilot for creative commit messages
```

## Copilot CLI Extension (MCP Server)

GitPet can run as an MCP (Model Context Protocol) server inside **GitHub Copilot CLI** chat.

### Quickstart (最簡單的方式)

**Step 1 — Build**

```bash
git clone https://github.com/<owner>/gh-pet.git
cd gh-pet
go build -o gitpet-mcp ./cmd/mcp/
```

**Step 2 — Register**

打開 Copilot CLI，輸入：

```
/mcp add gitpet stdio /full/path/to/gh-pet/gitpet-mcp
```

或者手動編輯 `~/.copilot/mcp-config.json`：

```json
{
  "mcpServers": {
    "gitpet": {
      "type": "stdio",
      "command": "/full/path/to/gh-pet/gitpet-mcp"
    }
  }
}
```

**Step 3 — 使用**

啟動 Copilot CLI，直接用自然語言互動：

```
$ copilot

> 幫我看一下 GitPet 狀態
> 餵一下我的 GitPet
> 幫我想幾個有創意的 commit message
```

Copilot 會自動呼叫對應的 MCP 工具（`pet_status`、`pet_feed`、`pet_suggest`）。

### Repo-Level Config（在專案內自動載入）

本 repo 已包含 `.copilot/mcp-config.json`，在此目錄下啟動 Copilot CLI 會自動載入 GitPet MCP server：

```bash
cd gh-pet
copilot
> 看看我的寵物
```

### 可用工具

| Tool | Description |
|------|-------------|
| `pet_status` | 查看 GitPet 的進化、心情、善良值、邏輯碎片和近 7 天活動 |
| `pet_feed` | 同步你的 GitHub 活動（commits、PRs、reviews）來餵食寵物 |
| `pet_suggest` | 根據寵物的性格和心情，產生創意 commit messages |

## Copilot Chat Extension (Vercel)

Deploy the Vercel Go handler in `api/handler.go`, then register the endpoint in your Copilot Extension configuration to enable `@gitpet status`.

Request body (minimal):
```json
{ "input": "@gitpet status", "user": { "login": "octocat" } }
```

Optional headers:
- `Authorization: Bearer <token>` or `X-GitHub-Token: <token>` for private activity access.

## Notes

- Pet state is stored at `~/.config/gh/gh-pet.json`.
- `feed` uses your GitHub events (last 7 days) plus local `git status/diff` for Thought Fragments.
